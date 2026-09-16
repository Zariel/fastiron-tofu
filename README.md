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
| Default VLAN selection | Implemented | [Global default ID](docs/guides/vlans.md), including 4095; preserves the default VLAN and associated VE settings. Deletion resets to VLAN 1. |
| VLAN membership | Implemented | Individual tagged or untagged Ethernet and LAG memberships. |
| IGMP snooping | Implemented | [Global/VLAN mode and version, plus native queries](docs/guides/igmp-snooping.md). Lifecycle, reboot and discovery validated; default VLANs 1 and 4095 covered. Explicit per-VLAN disabling and other multicast controls are outside this RESTCONF scope. |
| Interface voice VLAN | Implemented | [Local Ethernet policy and native query](docs/guides/voice-vlan.md), with lifecycle and reboot validation. FlexAuth voice actions and LLDP-MED remain separate. |
| Management VLAN selection | Not yet | Unavailable through the tested router image's documented RESTCONF paths; see [compatibility limits](docs/compatibility.md). |
| LAGs | Partial | [Static/dynamic mode, name, members, interface description and administrative state](docs/guides/lags.md), with native queries and reported interface status/counters. Mode replacement with STP policy is verified. Detached ports become disabled. Retained entries can require CLI restoration after external deletion. |
| Ethernet interfaces | Partial | Port name and administrative enable state; [single and inventory queries](docs/guides/ethernet.md) with status, link reports and precise counters. Speed, duplex and clock writes await SSH support because RESTCONF cannot reliably reset them; jumbo mode is global. |
| Protected ports | Implemented | [Ethernet and LAG isolation configuration](docs/guides/protected-ports.md) and native query, with lifecycle and reboot validation. Separate ownership from interface settings and LAG membership. |
| Storm control | Implemented | [Ethernet and LAG rate policies](docs/guides/storm-control.md) with a shared unit, per-class limits and native query. Logging, threshold and shutdown options are not managed. |
| Jumbo frames | Implemented | [Global configuration and native query](docs/guides/jumbo.md). Changes require saving and reloading; active mode and reload requirements are reported. Per-interface MTUs remain separate. |
| DSCP trust | Implemented | [Native trust configuration and query](docs/guides/dscp-trust.md), with ownership separate from QoS mappings and flow control. Ethernet lifecycle and reboot persistence verified; the tested LAG RESTCONF path is unavailable. |
| Routed VLAN interfaces (VE) | Implemented RESTCONF scope | [Existence, VLAN binding, port name and native query](docs/guides/ve.md); lifecycle and reboot persistence verified. Administrative state is readable; RESTCONF administrative writes had no native effect on tested firmware. |
| Other interface configuration | Not yet | Dedicated management interfaces, loopbacks, group-VE and tunnels remain. LAG interface description and administrative state are covered above. |
| Interface IP addresses | Implemented | Individual IPv4/IPv6 addresses on VE and management interfaces. |
| Static routes | Partial | [Default-VRF IPv4 gateway routes](docs/guides/routes.md), native queries and drift recovery. Lifecycle and reboot persistence verified. IPv6 and other route types remain. |
| OSPF | Partial | Default-VRF areas, interface bindings, and single/all-area queries. VE lifecycle and reboot persistence verified; additional routing options remain. |
| Spanning tree | Partial | [Native queries, VLAN STP/RSTP mode and priority, and Ethernet/LAG protection flags](docs/guides/spanning-tree.md), with lifecycle, drift recovery and reboot validation. Global STP and MST remain. |
| DNS servers | Partial | IPv4 servers and discovery. The tested firmware rejected IPv6 DNS addresses. |
| LLDP | Partial | [Global and Ethernet enable state, MED policies, and native queries](docs/guides/lldp.md), with lifecycle and reboot validation. Neighbor discovery remains. |
| PoE | Implemented RESTCONF scope | [Enable state, priority, allocation class and power limits](docs/guides/poe.md), including LAG members, native queries and reported power measurements. Lifecycle and reboot persistence verified; measurements under load remain unvalidated. |
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

Enter the pinned tool environment with `nix develop`. It includes Go, gofumpt, gopls, Ragel, OpenTofu, Make, Jujutsu, Git, curl, jq, Python, Poppler PDF utilities, OpenSSH, picocom, socat, and nixfmt.

```sh
nix develop
make check
make build
```

The native configuration parser is generated from [`config.rl`](internal/config/config.rl) (document framing) and [`command.rl`](internal/config/command.rl) (command grammar and argument capture). After editing a grammar, run `make generate` inside `nix develop` and commit the regenerated Go files. This invokes the `go:generate` directives in [`config.go`](internal/config/config.go). Go extractors apply semantic checks, defaults, and resource ownership to the parsed commands.

Use `gofumpt -w .` for Go and `nix fmt` for the flake. Run repeatable console command batches with `python3 tools/serial_console.py --device /dev/ttyUSB0 'show version'`. The script handles login, privilege elevation, prompt framing, timeouts, and redaction. Credentials come from the same `FASTIRON_*` environment variables as the provider. Serial device permissions are managed by the host operating system.

See [local installation and testing](docs/guides/development.md) and the [basic example](examples/basic/main.tf).
