package netbox

import (
	"fmt"
	"log/slog"
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
	model_ := device.Model
	if model_ == "" {
		model_ = device.Platform // fallback: use platform name as model
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

		for _, ip := range iface.IPAddresses {
			nbIP, err := client.FindOrCreateIPAddress(
				ip.Address, ip.Description, ip.Family, nbIface.ID,
			)
			if err != nil {
				log.Warn("skipping ip-address", "address", ip.Address, "err", err)
				continue
			}

			if primaryIPv4ID == 0 && ip.Family == 4 && !isLinkLocal(ip.Address) {
				if opts.PrimaryIPFamily == 0 || opts.PrimaryIPFamily == 4 {
					primaryIPv4ID = nbIP.ID
				}
			}
			if primaryIPv6ID == 0 && ip.Family == 6 && !isLinkLocal(ip.Address) {
				if opts.PrimaryIPFamily == 0 || opts.PrimaryIPFamily == 6 {
					primaryIPv6ID = nbIP.ID
				}
			}
		}
	}

	// 7. Set primary IPs
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

// isLinkLocal returns true for fe80::/10 and 169.254.0.0/16 addresses.
func isLinkLocal(cidr string) bool {
	addr := strings.SplitN(cidr, "/", 2)[0]
	return strings.HasPrefix(strings.ToLower(addr), "fe80:") ||
		strings.HasPrefix(addr, "169.254.")
}
