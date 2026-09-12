# fastiron-tofu

An OpenTofu provider for Brocade/RUCKUS FastIron switches. Declare switch configuration in HCL, review changes with `tofu plan`, and apply them with `tofu apply`.

The provider is under development. Configuration currently uses RESTCONF, with SSH for discovery, verification and saving. Features target firmware capabilities without model allowlists or required version settings.

Provider address: `registry.opentofu.org/zariel/fastiron`.
Plugin binary: `terraform-provider-fastiron`.

## Features

“Implemented” applies to the scope described in each row. “Partial” means part of the feature is available; the notes identify the remaining work.

| Feature | Supported / implemented | Notes |
|---|---|---|
| VLANs | Implemented | Create, name and delete VLANs; [single and collection discovery](docs/guides/vlans.md), including the default VLAN. |
| VLAN membership | Implemented | Individual tagged or untagged Ethernet and LAG memberships. |
| Additional VLAN policies | Not yet | Management/default VLAN selection, voice VLANs and IGMP snooping. |
| LAGs | Implemented | Static/dynamic mode, name, members and discovery. Detached ports follow native behavior and become disabled. |
| Ethernet interfaces | Partial | Port name and administrative enable state. Speed, duplex, clock, storm control, protected ports, jumbo frames and DSCP trust remain. |
| Routed VLAN interfaces (VE) | Partial | Existence, VLAN binding and port name. Administrative enable state remains. |
| Other interface configuration | Not yet | Dedicated management/LAG interface resources, loopbacks, group-VE and tunnels remain. |
| Interface IP addresses | Implemented | Individual IPv4/IPv6 addresses on VE and management interfaces. |
| Static routes | Partial | IPv4 prefix/gateway routes in the default VRF. Other route types remain. |
| OSPF | Partial | Default-VRF areas and interface bindings. Additional routing options remain. |
| Spanning tree | Partial | Per-VLAN STP/RSTP mode and priority; Ethernet admin-edge, BPDU guard and root guard. Global STP and MST remain. |
| DNS servers | Partial | IPv4 servers and discovery. The tested firmware rejected IPv6 DNS addresses. |
| LLDP | Partial | Global and Ethernet enable state; interface discovery. LLDP-MED network policies remain. |
| PoE | Partial | Ethernet enable state and reported measurements. Broader controls and validation under load remain. |
| Local users | Implemented | Usernames, privileges and passwords; privilege discovery. External password changes cannot currently be detected. |
| RADIUS/TACACS servers | Partial | Addresses, protocol ports, purposes and shared keys. Keyless TACACS, additional options and remote secret drift detection remain. |
| AAA policy | Partial | Ordered login methods, default dot1x authentication and CoA settings. Extended authentication services remain. |
| FlexAuth interfaces | Partial | Dot1x/MAC enablement, port-control, discovery, and basic global action reapplication. Voice-VLAN action variants remain. |
| FlexAuth global policy | Partial | Global VLANs, order, basic actions, enablement, MAC options, session limit, reauthentication and discovery. Guest-VLAN writes, voice action variants and additional timers remain. |
| IPv4 standard ACLs | Implemented | [Numbered ACLs](docs/guides/acls.md) with ordered source-prefix rules, including empty ACLs. Named standard ACLs are unavailable through the documented RESTCONF API. |
| IPv4 extended ACLs | Implemented | [Ordered rules](docs/guides/acls.md) with prefixes, protocol numbers, TCP/UDP ports and DSCP/priority markings. Named/numbered and empty ACLs; logging is unavailable through the tested RESTCONF API. |
| IPv6 ACLs | Implemented | [Ordered IPv6 rules](docs/guides/acls.md) with prefixes, protocols, TCP/UDP ports, markings and syslog actions. Named and empty ACLs, with lifecycle, reference and reboot validation. |
| MAC ACLs | Implemented | [Ordered MAC rules](docs/guides/acls.md) with address masks, EtherType and logging. Lifecycle, reboot, post-reboot updates and bound replacement validated. |
| ACL bindings | Implemented | [IPv4, IPv6 and MAC access groups](docs/guides/acls.md) on Ethernet, LAG and whole VLANs, with lifecycle and reboot validation. Logging and VLAN port subsets are excluded. |
| ACL discovery | Implemented | [Identity inventory and typed rule queries](docs/guides/acls.md#acl-inventory) for all four ACL kinds. Individual queries reject unsupported rule options; inventory includes them. |
| BGP | Not yet | Planned for SSH configuration support. |
| Routing policy | Not yet | Prefix lists, route maps, AS-path filters and community lists. |
| Global system settings | Not yet | Hostname and other system configuration fields. |
| NTP and remote logging | Not yet | Time servers and logging destinations. |
| SNMP | Not yet | Server groups, users and collectd configuration. |
| DHCP | Not yet | Server, client and relay helper configuration. |
| DHCP and neighbor security | Not yet | DHCPv4/v6 snooping, ARP/IPv6 neighbor inspection and IP source guards. |
| MAC address tables | Not yet | Static entries and static/dynamic table discovery. |
| Stacking | Not yet | Stack configuration. |
| Configuration saving | Implemented | Automatic saves or an explicit save resource. Saved contents are verified; ACL and binding reboot validation passed. |
| Firmware discovery | Implemented | Reports the active firmware; no expected-version input. |
| RESTCONF configuration | Partial | Used by the available resources above. Remaining endpoint and data-source coverage is still being implemented. |
| SSH discovery and verification | Implemented | Native configuration reads, firmware discovery, ownership checks and saving. |
| SSH configuration and fallback | Not yet | Follows completion of RESTCONF support. |
| Configuration backups | Not yet | Backup resources and policies remain. |
| Additional operational APIs | Not yet | Boot/reload, firmware transfer, diagnostics and configuration-file operations. |

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
