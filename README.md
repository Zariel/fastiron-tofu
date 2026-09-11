# fastiron-tofu

An OpenTofu provider for Brocade/RUCKUS FastIron switches. Declare switch configuration in HCL, review changes with `tofu plan`, and apply them with `tofu apply`.

The provider is under development. Configuration currently uses RESTCONF, with SSH for discovery, verification and saving. Features target firmware capabilities without model allowlists or required version settings.

Provider address: `registry.opentofu.org/zariel/fastiron`.
Plugin binary: `terraform-provider-fastiron`.

## Features

“Implemented” applies to the scope described in each row. “Partial” means part of the feature is available; the notes identify the remaining work.

| Feature | Supported / implemented | Notes |
|---|---|---|
| VLANs | Implemented | Create, name and delete VLANs. |
| VLAN membership | Implemented | Individual tagged or untagged Ethernet and LAG memberships. |
| LAGs | Implemented | Static/dynamic mode, name, members and discovery. Detached ports follow native behavior and become disabled. |
| Ethernet interfaces | Partial | Port name and administrative enable state. Speed, duplex and clock settings remain. |
| Routed VLAN interfaces (VE) | Partial | Existence, VLAN binding and port name. Administrative enable state remains. |
| Other interface configuration | Not yet | Dedicated management/LAG interface resources, loopbacks, group-VE and tunnels remain. |
| Interface IP addresses | Implemented | Individual IPv4/IPv6 addresses on VE and management interfaces. |
| Static routes | Partial | IPv4 prefix/gateway routes in the default VRF. Other route types remain. |
| OSPF | Partial | Default-VRF areas and interface bindings. Additional routing options remain. |
| Spanning tree | Partial | Per-VLAN STP/RSTP mode and priority; Ethernet admin-edge, BPDU guard and root guard. Global STP and MST remain. |
| DNS servers | Partial | IPv4 servers and discovery. The tested firmware rejected IPv6 DNS addresses. |
| LLDP | Implemented | Global and Ethernet enable state; interface discovery. |
| PoE | Partial | Ethernet enable state and reported measurements. Broader controls and validation under load remain. |
| Local users | Implemented | Usernames, privileges and passwords; privilege discovery. External password changes cannot currently be detected. |
| RADIUS/TACACS servers | Partial | Addresses, protocol ports, purposes and shared keys. Keyless TACACS, additional options and remote secret drift detection remain. |
| AAA policy | Partial | Ordered login methods, default dot1x authentication and CoA settings. Extended authentication services remain. |
| FlexAuth interfaces | Partial | Dot1x/MAC enablement, port-control, discovery, and basic global action reapplication. Voice-VLAN action variants remain. |
| FlexAuth global policy | Partial | Global VLANs, order, basic actions, enablement, MAC options, session limit, reauthentication and discovery. Guest-VLAN writes, voice action variants and additional timers remain. |
| ACLs and bindings | Partial | [Numbered IPv4 standard ACLs and access groups](docs/guides/acls.md). Ethernet/LAG bindings support IPv4, IPv6 and MAC. Other ACL definitions, VLAN/VE bindings and additional options remain. |
| BGP | Not yet | Planned for SSH configuration support. |
| Routing policy | Not yet | Prefix lists, route maps, AS-path filters and community lists. |
| Global system settings | Not yet | Hostname and other system configuration fields. |
| Configuration saving | Implemented | Automatic saves or an explicit save resource. Saved contents are verified; reboot persistence testing remains. |
| Firmware discovery | Implemented | Reports the active firmware; no expected-version input. |
| RESTCONF configuration | Partial | Used by the available resources above. Remaining endpoint and data-source coverage is still being implemented. |
| SSH discovery and verification | Implemented | Native configuration reads, firmware discovery, ownership checks and saving. |
| SSH configuration and fallback | Not yet | Follows completion of RESTCONF support. |
| Configuration backups | Not yet | Backup resources and policies remain. |

Hardware testing has used FastIron `09.0.10kT213` on an ICX 7150. ICX 7250 testing has not yet run. See [tested compatibility and limits](docs/compatibility.md) for details, and the [basic example](examples/basic/main.tf) to get started.

## Development

Enter the pinned tool environment with `nix develop`. It includes Go, gofumpt, gopls, OpenTofu, Make, Jujutsu, Git, curl, jq, Python, Poppler PDF utilities, OpenSSH, picocom, socat, and nixfmt.

```sh
nix develop
make check
make build
```

Use `gofumpt -w .` for Go and `nix fmt` for the flake. Run repeatable console command batches with `python3 tools/serial_console.py --device /dev/ttyUSB0 'show version'`. The script handles login, privilege elevation, prompt framing, timeouts, and redaction. Credentials come from the same `FASTIRON_*` environment variables as the provider. Serial device permissions are managed by the host operating system.

See [local installation and testing](docs/guides/development.md) and the [basic example](examples/basic/main.tf).
