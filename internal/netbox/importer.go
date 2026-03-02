package netbox

import (
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/buraglio/net2nbox/internal/model"
)

// ImportOptions controls the behaviour of an Import run.
type ImportOptions struct {
	// Site is the NetBox site name/slug where devices will be placed.
	// Must already exist in NetBox.
	Site string
	// Role is the device role name. Auto-created if absent. Defaults to "Router".
	Role string
	// DeviceType overrides the NetBox device-type model string. When empty,
	// the parsed device.Model is used, falling back to device.Platform.
	DeviceType string
	// PrimaryIPFamily selects which address family to promote as the device's
	// primary IP (4 or 6). 0 means "first non-link-local address found".
	PrimaryIPFamily int
}

// Import pushes device data into NetBox, creating or updating objects as needed.
func Import(client *Client, device *model.DeviceData, opts ImportOptions) error {
	log := slog.Default()

	if opts.Role == "" {
		opts.Role = "Router"
	}

	log.Info("starting import", "device", device.Summary())

	// 1. Manufacturer
	mfr, err := client.FindOrCreateManufacturer(device.Vendor)
	if err != nil {
		return fmt.Errorf("manufacturer: %w", err)
	}

	// 2. Device type
	model_ := opts.DeviceType
	if model_ == "" {
		model_ = device.Model
	}
	if model_ == "" {
		model_ = device.Platform
	}
	dt, err := client.FindOrCreateDeviceType(mfr.ID, model_)
	if err != nil {
		return fmt.Errorf("device-type: %w", err)
	}

	// 3. Device role
	role, err := client.FindOrCreateDeviceRole(opts.Role)
	if err != nil {
		return fmt.Errorf("device-role: %w", err)
	}

	// 4. Site
	site, err := client.FindSite(opts.Site)
	if err != nil {
		return fmt.Errorf("site: %w", err)
	}

	// 5. Device
	dev, err := client.FindOrCreateDevice(
		device.Hostname, device.SerialNumber,
		dt.ID, role.ID, site.ID,
	)
	if err != nil {
		return fmt.Errorf("device: %w", err)
	}
	log.Info("device ready", "id", dev.ID, "name", dev.Name)

	// 6. Interfaces + IPs
	var primaryIPv4ID, primaryIPv6ID int
	var fallbackIPv4ID, fallbackIPv6ID int

	nbIfaces := make(map[string]*Interface) // interface name → NetBox object

	for _, iface := range device.Interfaces {
		nbIface, err := client.FindOrCreateInterface(
			dev.ID,
			iface.Name, iface.Type, iface.Description, iface.MACAddress,
			iface.MTU, iface.Enabled,
		)
		if err != nil {
			log.Warn("skipping interface", "name", iface.Name, "err", err)
			continue
		}
		nbIfaces[iface.Name] = nbIface

		loopback := isLoopbackInterface(iface.Name)

		for _, ip := range iface.IPAddresses {
			if prefix := cidrToPrefix(ip.Address); prefix != "" {
				if _, err := client.FindOrCreatePrefix(prefix); err != nil {
					log.Warn("skipping prefix", "prefix", prefix, "err", err)
				}
			}

			nbIP, err := client.FindOrCreateIPAddress(
				ip.Address, ip.Description, ip.Family, nbIface.ID,
			)
			if err != nil {
				log.Warn("skipping ip-address", "address", ip.Address, "err", err)
				continue
			}

			if ip.Family == 4 && !isLinkLocal(ip.Address) &&
				(opts.PrimaryIPFamily == 0 || opts.PrimaryIPFamily == 4) {
				if loopback && primaryIPv4ID == 0 {
					primaryIPv4ID = nbIP.ID
				} else if fallbackIPv4ID == 0 {
					fallbackIPv4ID = nbIP.ID
				}
			}
			if ip.Family == 6 && !isLinkLocal(ip.Address) &&
				(opts.PrimaryIPFamily == 0 || opts.PrimaryIPFamily == 6) {
				if loopback && primaryIPv6ID == 0 {
					primaryIPv6ID = nbIP.ID
				} else if fallbackIPv6ID == 0 {
					fallbackIPv6ID = nbIP.ID
				}
			}
		}
	}

	if primaryIPv4ID == 0 {
		primaryIPv4ID = fallbackIPv4ID
	}
	if primaryIPv6ID == 0 {
		primaryIPv6ID = fallbackIPv6ID
	}

	// 7. LLDP cables
	for _, iface := range device.Interfaces {
		if len(iface.LLDPNeighbors) == 0 {
			continue
		}
		nbIface, ok := nbIfaces[iface.Name]
		if !ok || nbIface.Cable != nil {
			continue // interface not created or already cabled
		}
		for _, nbr := range iface.LLDPNeighbors {
			if err := importLLDPCable(client, log, nbIface.ID, nbr); err != nil {
				log.Warn("skipping lldp cable", "local_iface", iface.Name,
					"remote_sys", nbr.RemoteSysName, "err", err)
			}
		}
	}

	// 8. Set primary IPs
	if primaryIPv4ID > 0 && !client.DryRun {
		if err := client.SetPrimaryIP(dev.ID, primaryIPv4ID, 4); err != nil {
			log.Warn("could not set primary IPv4", "err", err)
		} else {
			log.Info("set primary IPv4", "ip_id", primaryIPv4ID)
		}
	}
	if primaryIPv6ID > 0 && !client.DryRun {
		if err := client.SetPrimaryIP(dev.ID, primaryIPv6ID, 6); err != nil {
			log.Warn("could not set primary IPv6", "err", err)
		} else {
			log.Info("set primary IPv6", "ip_id", primaryIPv6ID)
		}
	}

	log.Info("import complete", "device", device.Hostname)
	return nil
}

// importLLDPCable resolves the remote endpoint from an LLDP neighbor record and
// creates a cable between the local interface and the remote interface in NetBox.
func importLLDPCable(client *Client, log *slog.Logger, localIfaceID int, nbr model.LLDPNeighbor) error {
	remDev, err := client.FindDevice(nbr.RemoteSysName)
	if err != nil {
		return err
	}
	if remDev == nil {
		log.Debug("lldp remote device not in NetBox", "sys_name", nbr.RemoteSysName)
		return nil
	}

	// Try RemotePortID first, then RemotePortDesc as fallback.
	var remIface *Interface
	for _, name := range []string{nbr.RemotePortID, nbr.RemotePortDesc} {
		if name == "" {
			continue
		}
		remIface, err = client.FindInterface(remDev.ID, name)
		if err == nil && remIface != nil {
			break
		}
	}
	if remIface == nil {
		log.Debug("lldp remote interface not in NetBox",
			"sys_name", nbr.RemoteSysName, "port_id", nbr.RemotePortID)
		return nil
	}
	if remIface.Cable != nil {
		log.Debug("lldp remote interface already cabled", "iface_id", remIface.ID)
		return nil
	}

	_, err = client.FindOrCreateCable(localIfaceID, remIface.ID)
	return err
}

// isLoopbackInterface returns true for loopback interface names.
func isLoopbackInterface(name string) bool {
	ln := strings.ToLower(name)
	return strings.HasPrefix(ln, "loopback") || ln == "lo" || ln == "lo0"
}

// isLinkLocal returns true for fe80::/10 and 169.254.0.0/16 addresses.
func isLinkLocal(cidr string) bool {
	addr := strings.SplitN(cidr, "/", 2)[0]
	return strings.HasPrefix(strings.ToLower(addr), "fe80:") ||
		strings.HasPrefix(addr, "169.254.")
}

// cidrToPrefix returns the network address for a host CIDR (e.g. "10.0.0.1/24" → "10.0.0.0/24").
// Returns "" if the input cannot be parsed.
func cidrToPrefix(cidr string) string {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return ""
	}
	return ipnet.String()
}
