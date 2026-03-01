// Package snmp provides optional SNMP-based augmentation of parsed device data.
// It queries standard MIB-II OIDs plus vendor-specific extensions (MikroTik)
// and merges the results into a model.DeviceData.
//
// Standard OIDs used:
//
//	sysDescr   1.3.6.1.2.1.1.1.0
//	sysName    1.3.6.1.2.1.1.5.0
//	ifName     1.3.6.1.2.1.31.1.1.1.1   (table)
//	ifPhysAddr 1.3.6.1.2.1.2.2.1.6      (table)
//	ifHighSpeed 1.3.6.1.2.1.31.1.1.1.15 (table, Mbps)
//
// MikroTik-specific OIDs (enterprise 1.3.6.1.4.1.14988):
//
//	mtxrSerialNumber 1.3.6.1.4.1.14988.1.1.7.3.0
//	mtxrBoardName    1.3.6.1.4.1.14988.1.1.7.8.0
//	mtxrFirmwareVersion 1.3.6.1.4.1.14988.1.1.7.4.0
package snmp

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/buraglio/net2nbox/internal/model"
	"github.com/gosnmp/gosnmp"
)

const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
	oidIfName      = "1.3.6.1.2.1.31.1.1.1.1"
	oidIfPhysAddr  = "1.3.6.1.2.1.2.2.1.6"
	oidIfHighSpeed = "1.3.6.1.2.1.31.1.1.1.15"

	// MikroTik enterprise OIDs
	oidMtxrSerial    = "1.3.6.1.4.1.14988.1.1.7.3.0"
	oidMtxrBoardName = "1.3.6.1.4.1.14988.1.1.7.8.0"
	oidMtxrFirmware  = "1.3.6.1.4.1.14988.1.1.7.4.0"
)

// Config holds SNMP connection parameters.
type Config struct {
	Target    string        // hostname or IP
	Port      uint16        // default 161
	Community string        // SNMPv2c community
	Version   string        // "2c" (default) or "3"
	Timeout   time.Duration // default 5s
	Retries   int           // default 2
}

// Collector handles SNMP queries against a single device.
type Collector struct {
	cfg  Config
	g    *gosnmp.GoSNMP
	log  *slog.Logger
}

// New creates a Collector with the given config.
func New(cfg Config) *Collector {
	if cfg.Port == 0 {
		cfg.Port = 161
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Retries == 0 {
		cfg.Retries = 2
	}
	if cfg.Community == "" {
		cfg.Community = "public"
	}

	ver := gosnmp.Version2c
	if cfg.Version == "1" {
		ver = gosnmp.Version1
	}

	g := &gosnmp.GoSNMP{
		Target:    cfg.Target,
		Port:      cfg.Port,
		Community: cfg.Community,
		Version:   ver,
		Timeout:   cfg.Timeout,
		Retries:   cfg.Retries,
	}
	return &Collector{cfg: cfg, g: g, log: slog.Default()}
}

// Connect opens the SNMP UDP session.
func (c *Collector) Connect() error {
	return c.g.Connect()
}

// Close closes the SNMP session.
func (c *Collector) Close() {
	if c.g.Conn != nil {
		c.g.Conn.Close()
	}
}

// Augment enriches device with SNMP-discovered data.
// Fields already populated in device are preserved; SNMP data fills gaps.
func (c *Collector) Augment(device *model.DeviceData) error {
	// System scalars
	if err := c.augmentSystem(device); err != nil {
		c.log.Warn("SNMP system query failed", "err", err)
	}

	// MikroTik-specific
	if device.Vendor == "MikroTik" || device.Platform == "RouterOS" {
		if err := c.augmentMikrotik(device); err != nil {
			c.log.Warn("MikroTik SNMP query failed", "err", err)
		}
	}

	// Interface table
	if err := c.augmentInterfaces(device); err != nil {
		c.log.Warn("SNMP interface query failed", "err", err)
	}

	return nil
}

func (c *Collector) augmentSystem(device *model.DeviceData) error {
	result, err := c.g.Get([]string{oidSysDescr, oidSysName})
	if err != nil {
		return err
	}
	for _, v := range result.Variables {
		switch v.Name {
		case "." + oidSysDescr:
			descr := string(v.Value.([]byte))
			c.log.Debug("sysDescr", "value", descr)
			// Extract version from sysDescr if not already set
			if device.OSVersion == "" {
				device.OSVersion = extractVersion(descr)
			}
		case "." + oidSysName:
			if device.Hostname == "" {
				device.Hostname = string(v.Value.([]byte))
			}
		}
	}
	return nil
}

func (c *Collector) augmentMikrotik(device *model.DeviceData) error {
	oids := []string{oidMtxrSerial, oidMtxrBoardName, oidMtxrFirmware}
	result, err := c.g.Get(oids)
	if err != nil {
		return err
	}
	for _, v := range result.Variables {
		if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
			continue
		}
		val := ""
		switch v.Value.(type) {
		case []byte:
			val = string(v.Value.([]byte))
		case string:
			val = v.Value.(string)
		}
		switch v.Name {
		case "." + oidMtxrSerial:
			if device.SerialNumber == "" {
				device.SerialNumber = strings.TrimSpace(val)
			}
		case "." + oidMtxrBoardName:
			if device.Model == "" {
				device.Model = strings.TrimSpace(val)
			}
		case "." + oidMtxrFirmware:
			if device.OSVersion == "" {
				device.OSVersion = strings.TrimSpace(val)
			}
		}
	}
	return nil
}

// augmentInterfaces walks the interface tables and updates matching entries
// in device.Interfaces by name.
func (c *Collector) augmentInterfaces(device *model.DeviceData) error {
	// Build a name→index map from ifName table
	ifNames, err := c.walkTable(oidIfName)
	if err != nil {
		return fmt.Errorf("walk ifName: %w", err)
	}
	// index→MAC
	ifMACs, _ := c.walkTable(oidIfPhysAddr)
	// index→speed (Mbps)
	ifSpeeds, _ := c.walkTable(oidIfHighSpeed)

	// Build lookup by interface name
	type snmpIf struct {
		mac   string
		speed uint64
	}
	byName := make(map[string]snmpIf)
	for idx, name := range ifNames {
		si := snmpIf{}
		if mac, ok := ifMACs[idx]; ok {
			si.mac = formatMAC(mac)
		}
		if speed, ok := ifSpeeds[idx]; ok {
			if u, ok := speed.(uint); ok {
				si.speed = uint64(u)
			}
			if u, ok := speed.(uint64); ok {
				si.speed = u
			}
		}
		nameStr := ""
		switch name.(type) {
		case []byte:
			nameStr = string(name.([]byte))
		case string:
			nameStr = name.(string)
		}
		byName[nameStr] = si
	}

	// Merge into device interfaces
	for i := range device.Interfaces {
		iface := &device.Interfaces[i]
		if si, ok := byName[iface.Name]; ok {
			if iface.MACAddress == "" && si.mac != "" {
				iface.MACAddress = si.mac
			}
			// Refine type based on SNMP speed if still at default
			if si.speed > 0 {
				iface.Type = speedToType(si.speed, iface.Type)
			}
		}
	}
	return nil
}

// walkTable walks an SNMP table OID and returns a map of instance index → value.
func (c *Collector) walkTable(tableOID string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	err := c.g.Walk(tableOID, func(pdu gosnmp.SnmpPDU) error {
		// Strip the table OID prefix to get the instance index
		idx := strings.TrimPrefix(pdu.Name, "."+tableOID+".")
		result[idx] = pdu.Value
		return nil
	})
	return result, err
}

// speedToType maps SNMP ifHighSpeed (Mbps) to a NetBox interface type slug,
// but only refines types that are currently set to a default.
func speedToType(speedMbps uint64, currentType string) string {
	// Don't override explicit non-default types
	if currentType != "1000base-t" && currentType != "other" {
		return currentType
	}
	switch {
	case speedMbps >= 400000:
		return "400gbase-x-qsfpdd"
	case speedMbps >= 100000:
		return "100gbase-x-qsfp28"
	case speedMbps >= 40000:
		return "40gbase-x-qsfpp"
	case speedMbps >= 25000:
		return "25gbase-x-sfp28"
	case speedMbps >= 10000:
		return "10gbase-x-sfpp"
	case speedMbps >= 2500:
		return "2.5gbase-t"
	case speedMbps >= 1000:
		return "1000base-t"
	case speedMbps >= 100:
		return "100base-tx"
	default:
		return currentType
	}
}

func formatMAC(v interface{}) string {
	switch val := v.(type) {
	case []byte:
		if len(val) == 6 {
			return strings.ToUpper(fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
				val[0], val[1], val[2], val[3], val[4], val[5]))
		}
		return strings.ToUpper(hex.EncodeToString(val))
	case string:
		return strings.ToUpper(val)
	}
	return ""
}

func extractVersion(sysDescr string) string {
	// RouterOS: "RouterOS 7.15.2 (stable)"
	// IOS: "Cisco IOS Software ... Version 17.x.y"
	// JunOS: "Juniper Networks ... JUNOS 20.4R3.8"
	lower := strings.ToLower(sysDescr)
	markers := []string{"version ", "routeros ", "junos "}
	for _, marker := range markers {
		if idx := strings.Index(lower, marker); idx >= 0 {
			rest := sysDescr[idx+len(marker):]
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return strings.Trim(fields[0], "(),")
			}
		}
	}
	return ""
}
