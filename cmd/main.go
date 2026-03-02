// net2nbox imports network device configuration files into NetBox via REST API.
//
// Usage:
//
//	net2nbox import --file router.rsc \
//	  --netbox-url https://netbox.example.com \
//	  --netbox-token <token> \
//	  --site dc1 \
//	  [--vendor mikrotik] \
//	  [--role Router] \
//	  [--dry-run]
//
//	net2nbox import --file config.txt --vendor cisco-iosxe \
//	  --netbox-url ... --netbox-token ... --site ...
//	  --snmp-host 192.0.2.1 --snmp-community public
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/buraglio/net2nbox/internal/model"
	"github.com/buraglio/net2nbox/internal/netbox"
	"github.com/buraglio/net2nbox/internal/snmp"
	"github.com/spf13/cobra"

	// Blank imports register each vendor parser into the default registry.
	_ "github.com/buraglio/net2nbox/internal/parser/arista"
	_ "github.com/buraglio/net2nbox/internal/parser/cisco/iosxe"
	_ "github.com/buraglio/net2nbox/internal/parser/cisco/iosxr"
	_ "github.com/buraglio/net2nbox/internal/parser/fs"
	_ "github.com/buraglio/net2nbox/internal/parser/junos"
	_ "github.com/buraglio/net2nbox/internal/parser/mikrotik"

	"github.com/buraglio/net2nbox/internal/parser"
)

func main() {
	root := buildRoot()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func buildRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "net2nbox",
		Short: "Import network device configs into NetBox",
		Long: `net2nbox parses vendor configuration files (MikroTik RouterOS, Cisco IOS-XE/XR,
JunOS, Arista EOS, FS FSOS) and imports the extracted data — interfaces, IPv4/IPv6
addresses, hardware details — into NetBox via the REST API.`,
	}

	root.AddCommand(buildImportCmd())
	root.AddCommand(buildParseCmd())
	root.AddCommand(buildVendorsCmd())
	return root
}

func buildImportCmd() *cobra.Command {
	var (
		file            string
		vendor          string
		netboxURL       string
		netboxToken     string
		site            string
		role            string
		deviceType      string
		dryRun          bool
		primaryIPFamily int
		jsonOutput      bool
		verbose         bool
		snmpHost        string
		snmpCommunity   string
		snmpVersion     string
		snmpLLDP        bool
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Parse a config file and import it into NetBox",
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogging(verbose)

			// Read config file
			content, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("reading %s: %w", file, err)
			}

			// Parse
			device, err := parser.DefaultRegistry.ParseFile(string(content), vendor)
			if err != nil {
				return fmt.Errorf("parse: %w", err)
			}

			// SNMP augmentation (optional)
			if snmpHost != "" {
				device, err = augmentViaSNMP(device, snmpHost, snmpCommunity, snmpVersion, snmpLLDP)
				if err != nil {
					slog.Warn("SNMP augmentation failed; continuing with config data only", "err", err)
				}
			}

			if jsonOutput {
				return printJSON(device)
			}

			if dryRun {
				slog.Info("=== DRY RUN — no changes will be written to NetBox ===")
			}

			// Validate required flags
			if netboxURL == "" || netboxToken == "" {
				return fmt.Errorf("--netbox-url and --netbox-token are required (unless using --parse-only)")
			}
			if site == "" {
				return fmt.Errorf("--site is required")
			}

			// Import
			client := netbox.New(netboxURL, netboxToken, dryRun)
			return netbox.Import(client, device, netbox.ImportOptions{
				Site:            site,
				Role:            role,
				DeviceType:      deviceType,
				PrimaryIPFamily: primaryIPFamily,
			})
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to the device configuration file (required)")
	cmd.Flags().StringVarP(&vendor, "vendor", "v", "",
		"Vendor/platform: mikrotik, cisco, cisco-iosxr, juniper, arista, fs (auto-detect if omitted)")
	cmd.Flags().StringVar(&netboxURL, "netbox-url", "", "NetBox base URL (e.g. https://netbox.example.com)")
	cmd.Flags().StringVar(&netboxToken, "netbox-token", "", "NetBox API token")
	cmd.Flags().StringVar(&site, "site", "", "NetBox site name or slug (must already exist)")
	cmd.Flags().StringVar(&role, "role", "Router", "NetBox device role (created if absent)")
	cmd.Flags().StringVar(&deviceType, "device-type", "", "NetBox device type model string (overrides parsed value)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print planned changes without modifying NetBox")
	cmd.Flags().IntVar(&primaryIPFamily, "primary-ip", 0,
		"Address family to promote as primary IP: 4, 6, or 0 (first non-link-local found)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print parsed device data as JSON and exit (no NetBox calls)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "V", false, "Enable debug-level logging")
	cmd.Flags().StringVar(&snmpHost, "snmp-host", "", "SNMP target host for augmentation")
	cmd.Flags().StringVar(&snmpCommunity, "snmp-community", "public", "SNMPv2c community string")
	cmd.Flags().StringVar(&snmpVersion, "snmp-version", "2c", "SNMP version: 1 or 2c")
	cmd.Flags().BoolVar(&snmpLLDP, "snmp-lldp", false, "Collect LLDP neighbors via SNMP and create cables in NetBox")

	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func buildParseCmd() *cobra.Command {
	var (
		file    string
		vendor  string
		verbose bool
	)
	cmd := &cobra.Command{
		Use:   "parse",
		Short: "Parse a config file and print the result as JSON (no NetBox calls)",
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogging(verbose)
			content, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("reading %s: %w", file, err)
			}
			device, err := parser.DefaultRegistry.ParseFile(string(content), vendor)
			if err != nil {
				return err
			}
			return printJSON(device)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Config file path (required)")
	cmd.Flags().StringVarP(&vendor, "vendor", "v", "", "Vendor override (optional)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "V", false, "Verbose output")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func buildVendorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "vendors",
		Short: "List registered vendor parsers",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Registered vendor parsers:")
			for _, v := range parser.DefaultRegistry.Names() {
				fmt.Printf("  %s\n", v)
			}
		},
	}
}

func augmentViaSNMP(device *model.DeviceData, host, community, version string, lldp bool) (*model.DeviceData, error) {
	col := snmp.New(snmp.Config{
		Target:    host,
		Community: community,
		Version:   version,
	})
	if err := col.Connect(); err != nil {
		return device, fmt.Errorf("SNMP connect to %s: %w", host, err)
	}
	defer col.Close()

	if err := col.Augment(device); err != nil {
		return device, err
	}
	if lldp {
		if err := col.CollectLLDP(device); err != nil {
			slog.Warn("LLDP collection failed", "err", err)
		}
	}
	slog.Info("SNMP augmentation complete", "host", host)
	return device, nil
}

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func setupLogging(verbose bool) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))
}
