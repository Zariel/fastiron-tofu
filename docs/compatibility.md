# Supported features and compatibility

The provider has no firmware or model allowlist and does not require an expected version. Availability depends on the switch's firmware, interfaces, and enabled services.

## Tested platform

Hardware testing has used FastIron `09.0.10kT213` (image label `SPR09010k`, build `SPR09010kB114ae.bin`) on an ICX 7150-C12. ICX 7250 testing has not yet run. These are tested versions, not requirements.

## Available configuration resources

| Area | Supported configuration |
|---|---|
| VLAN | Existence and name |
| VLAN membership | One tagged or untagged Ethernet or LAG relationship |
| LAG | Existence, name, dynamic/static mode, and Ethernet membership |
| Ethernet | Port name and administrative enable state |
| Routed VLAN interface | VE existence, VLAN binding, and port name |
| Interface addresses | Individual IPv4/IPv6 addresses on VE and management interfaces |
| Spanning tree | Per-VLAN STP/RSTP mode and bridge priority; Ethernet admin-edge, BPDU guard and root guard |
| OSPF | Default-VRF area existence and interface bindings |
| Static routing | One IPv4 prefix and gateway relationship in the default VRF |
| DNS | Individual server addresses |
| AAA servers | Individual RADIUS/TACACS server addresses, protocol ports, purposes and shared keys |
| LLDP | Global and Ethernet enable state |
| PoE | Ethernet administrative enable state |
| Persistence | Automatic saves or explicit configuration-save resource |

Data sources report active firmware, DNS servers, LLDP interface settings, PoE interface configuration and measurements, interface IP addresses, LAG configuration with Ethernet membership, IPv4 static routes, OSPF areas with interface bindings, VLAN/interface spanning-tree settings, and [RADIUS/TACACS server metadata](guides/aaa.md).

Configuration resources currently use RESTCONF. SSH is also required for firmware discovery, persistence, and parent-deletion checks. SSH configuration fallback is not yet available. Configure RESTCONF and its configuration synchronization on the switch before using these resources.

## Limits

- AAA user/policy configuration and keyless TACACS are not yet supported; see [AAA ownership and secret handling](guides/aaa.md).

- Ethernet speed, duplex and clock settings are not yet managed. On the tested `09.0.10kT213` build, RESTCONF auto-negotiation updates and individual leaf deletions did not restore native automatic speed. Deleting the Ethernet container restored speed but also changed an unrelated DHCP-client setting, so it is unsuitable for narrowly owned resource cleanup.
- Spanning tree currently manages per-VLAN STP/RSTP mode and priority, plus Ethernet admin-edge, BPDU guard and root guard; see [spanning-tree ownership and limits](guides/spanning-tree.md).
- OSPF currently manages area existence and interface bindings; see [OSPF ownership and limits](guides/ospf.md).
- Static routes currently support IPv4 gateways in the default VRF; see [route ownership and limits](guides/routes.md).
- VE administrative enable state is not currently managed.
- DNS IPv4 succeeded on the tested build; its endpoint rejected IPv6 DNS addresses.
- PoE tests used a disconnected port. Reported power measurements under load have not been validated.
- Reboot persistence testing has not yet completed; saved configuration was checked independently over serial.

Each resource owns only its documented settings. VLAN and VE deletion reject remaining child configuration. Avoid declaring the same remote object in multiple resources or states. Provider operations are serialized per hostname within one provider process; separate states, processes, DNS aliases, and external configuration changes require operator coordination.
