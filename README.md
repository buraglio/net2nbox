### net2nbox — Network Config to NetBox Importer

A Go tool that parses vendor network device configuration files and imports
the extracted data into NetBox via the REST API.

---

## Features

- Extracts: interfaces, IPv4/IPv6 addresses, MAC addresses, MTU, descriptions,
  serial numbers, model/hardware details, LAG members, bridge members, VLAN IDs
- Optional SNMP augmentation to fill gaps not present in config exports
- LLDP neighbor discovery via SNMP — automatically creates cables in NetBox
- Auto-detects vendor from config content
- Idempotent: finds existing NetBox objects before creating new ones
- Primary IP selection prefers loopback addresses; falls back to first non-link-local
- Automatic prefix management: each imported IP address is placed in its enclosing
  prefix; prefixes are created if absent and nested automatically by NetBox
- Dry-run mode: prints planned changes without touching NetBox
- `parse` sub-command: dumps parsed data as JSON for inspection

## Supported Vendors

| Vendor | Platform | Status |
|--------|----------|--------|
| MikroTik | RouterOS v7 | Full implementation |
| Cisco | IOS-XE | Functional |
| Cisco | IOS-XR | Functional |
| Juniper | JunOS (hierarchical + set format) | Functional |
| Arista | EOS | Functional |
| FS | FSOS | Functional |

## Installation

```bash
go install github.com/buraglio/net2nbox/cmd@latest
```

or build from source:

```bash
git clone https://github.com/buraglio/net2nbox
cd net2nbox
go build -o net2nbox ./cmd/
```

## Usage

### Inspect parsed data (no NetBox required)

```bash
net2nbox parse --file router.rsc
net2nbox parse --file router.rsc --vendor mikrotik
```

### Import to NetBox

```bash
net2nbox import \
  --file router.rsc \
  --netbox-url https://netbox.example.com \
  --netbox-token <your-token> \
  --site dc1 \
  --role Router \
  --dry-run          # remove to actually write
```

### Override device type

When the parsed model string does not match the NetBox device-type name, use
`--device-type` to supply the exact string:

```bash
net2nbox import \
  --file router.cfg \
  --netbox-url https://netbox.example.com \
  --netbox-token <token> \
  --site dc1 \
  --device-type "CCR1036-8G-2S+"
```

Resolution order: `--device-type` flag → parsed model field → platform name.

### Import with SNMP augmentation

SNMP fills in serial numbers, accurate interface speeds/types, and MAC addresses
that may not appear in a config export (especially useful for MikroTik where
`serial number` requires `/system routerboard print`).

```bash
net2nbox import \
  --file router.rsc \
  --netbox-url https://netbox.example.com \
  --netbox-token <token> \
  --site dc1 \
  --snmp-host 192.0.2.1 \
  --snmp-community public
```

### Import with LLDP neighbor discovery

Adding `--snmp-lldp` walks the LLDP-MIB on the target device and creates
cables in NetBox for each discovered neighbor whose device and interface are
already present. Interfaces that already have a cable attached are skipped.

```bash
net2nbox import \
  --file router.rsc \
  --netbox-url https://netbox.example.com \
  --netbox-token <token> \
  --site dc1 \
  --snmp-host 192.0.2.1 \
  --snmp-community public \
  --snmp-lldp
```

LLDP cable creation is best-effort: if the remote device or interface is not
yet in NetBox the entry is skipped with a debug log message and the import
continues normally.

### List registered vendor parsers

```bash
net2nbox vendors
```

## Flag Reference

| Flag | Default | Description |
|------|---------|-------------|
| `--file`, `-f` | *(required)* | Path to the device configuration file |
| `--vendor`, `-v` | auto-detect | Vendor/platform override: `mikrotik`, `cisco`, `cisco-iosxr`, `juniper`, `arista`, `fs` |
| `--netbox-url` | | NetBox base URL |
| `--netbox-token` | | NetBox API token |
| `--site` | *(required)* | NetBox site name or slug (must already exist) |
| `--role` | `Router` | NetBox device role (auto-created if absent) |
| `--device-type` | | NetBox device-type model string (overrides parsed value) |
| `--primary-ip` | `0` | Address family for primary IP: `4`, `6`, or `0` (loopback preferred, then first non-link-local) |
| `--dry-run` | `false` | Print planned changes without writing to NetBox |
| `--json` | `false` | Print parsed device data as JSON and exit (no NetBox calls) |
| `--verbose`, `-V` | `false` | Enable debug-level logging |
| `--snmp-host` | | SNMP target host for augmentation |
| `--snmp-community` | `public` | SNMPv2c community string |
| `--snmp-version` | `2c` | SNMP version: `1` or `2c` |
| `--snmp-lldp` | `false` | Collect LLDP neighbors via SNMP and create cables in NetBox |

## NetBox Prerequisites

- Site must already exist (`--site` matches by name or slug)
- Device role is auto-created if absent
- Manufacturer and device-type are auto-created if absent
- API token needs write access to `dcim` and `ipam`
- For cable creation (`--snmp-lldp`): both endpoints must already exist in NetBox

## Primary IP Selection

The importer promotes one IPv4 and one IPv6 address as the device's primary IPs
using this preference order:

1. First non-link-local address on a loopback interface (`loopback*`, `lo`, `lo0`)
2. First non-link-local address on any other interface

Use `--primary-ip 4` or `--primary-ip 6` to restrict selection to a single
address family.

## Project Structure

```
net2nbox/
├── cmd/main.go                          CLI (cobra)
├── internal/
│   ├── model/device.go                  Normalized data model
│   ├── parser/
│   │   ├── parser.go                    Parser interface + registry
│   │   ├── mikrotik/parser.go           RouterOS v7 (full)
│   │   ├── cisco/iosxe/parser.go        IOS-XE
│   │   ├── cisco/iosxr/parser.go        IOS-XR
│   │   ├── junos/parser.go              JunOS hierarchical + set
│   │   ├── arista/parser.go             EOS
│   │   └── fs/parser.go                 FSOS
│   ├── netbox/
│   │   ├── client.go                    NetBox REST API client
│   │   └── importer.go                  Import orchestration
│   └── snmp/collector.go                SNMP augmentation + LLDP collection
└── go.mod
```

## Adding a New Vendor

1. Create `internal/parser/<vendor>/parser.go`
2. Implement the three-method `parser.Parser` interface:
   ```go
   func (p *Parser) Vendor() string { return "MyVendor" }
   func (p *Parser) Detect(content string) bool { ... }
   func (p *Parser) Parse(content string) (*model.DeviceData, error) { ... }
   ```
3. Register in `init()`:
   ```go
   func init() { parser.DefaultRegistry.Register(&Parser{}) }
   ```
4. Blank-import the package in `cmd/main.go`

## SNMP OIDs Used

### Standard augmentation

| OID | Name | Purpose |
|-----|------|---------|
| `1.3.6.1.2.1.1.1.0` | sysDescr | Version extraction |
| `1.3.6.1.2.1.1.5.0` | sysName | Hostname fallback |
| `1.3.6.1.2.1.31.1.1.1.1` | ifName | Interface names |
| `1.3.6.1.2.1.2.2.1.6` | ifPhysAddress | MAC addresses |
| `1.3.6.1.2.1.31.1.1.1.15` | ifHighSpeed | Speed in Mbps → interface type |
| `1.3.6.1.4.1.14988.1.1.7.3.0` | mtxrSerialNumber | MikroTik serial |
| `1.3.6.1.4.1.14988.1.1.7.8.0` | mtxrBoardName | MikroTik model |
| `1.3.6.1.4.1.14988.1.1.7.4.0` | mtxrFirmwareVersion | RouterOS version |

### LLDP neighbor discovery (`--snmp-lldp`)

| OID | Name | Purpose |
|-----|------|---------|
| `1.3.6.1.2.1.88.1.3.7.1.3` | lldpLocPortId | Map local port numbers to interface names |
| `1.3.6.1.2.1.88.1.3.7.1.4` | lldpLocPortDesc | Fallback local port name |
| `1.3.6.1.2.1.88.1.4.1.1.7` | lldpRemPortId | Remote interface name |
| `1.3.6.1.2.1.88.1.4.1.1.8` | lldpRemPortDesc | Remote interface description (fallback) |
| `1.3.6.1.2.1.88.1.4.1.1.9` | lldpRemSysName | Remote device hostname |
