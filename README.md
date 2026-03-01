### net2nbox — Network Config to NetBox Importer

A Go tool that parses vendor network device configuration files and imports
the extracted data into NetBox via the REST API.

---

## Features

- Extracts: interfaces, IPv4/IPv6 addresses, MAC addresses, MTU, descriptions,
  serial numbers, model/hardware details, LAG members, bridge members, VLAN IDs
- Optional SNMP augmentation to fill gaps not present in config exports
- Auto-detects vendor from config content
- Idempotent: finds existing NetBox objects before creating new ones
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

### List registered vendor parsers

```bash
net2nbox vendors
```

## NetBox Prerequisites

- Site must already exist (`--site` matches by name or slug)
- Device role is auto-created if absent
- Manufacturer and device-type are auto-created if absent
- API token needs write access to `dcim` and `ipam`

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
│   └── snmp/collector.go                SNMP augmentation
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

| OID | Name | Notes |
|-----|------|-------|
| `1.3.6.1.2.1.1.1.0` | sysDescr | Version extraction |
| `1.3.6.1.2.1.1.5.0` | sysName | Hostname fallback |
| `1.3.6.1.2.1.31.1.1.1.1` | ifName | Interface names |
| `1.3.6.1.2.1.2.2.1.6` | ifPhysAddress | MAC addresses |
| `1.3.6.1.2.1.31.1.1.1.15` | ifHighSpeed | Speed in Mbps |
| `1.3.6.1.4.1.14988.1.1.7.3.0` | mtxrSerialNumber | MikroTik serial |
| `1.3.6.1.4.1.14988.1.1.7.8.0` | mtxrBoardName | MikroTik model |
| `1.3.6.1.4.1.14988.1.1.7.4.0` | mtxrFirmwareVersion | RouterOS version |
