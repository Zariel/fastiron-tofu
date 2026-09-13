# Supported features and compatibility

The provider has no firmware or model allowlist and does not require an expected version. Availability depends on the switch's firmware, interfaces, and enabled services.

## Tested platform

Hardware testing has used FastIron `09.0.10kT213` (image label `SPR09010k`, build `SPR09010kB114ae.bin`) on an ICX 7150-C12P. ICX 7250 testing has not yet run. These are tested versions, not requirements.

## Available configuration resources

| Area | Supported configuration |
|---|---|
| VLAN | Existence and name |
| VLAN membership | One tagged or untagged Ethernet or LAG relationship |
| IGMP snooping | Global mode/version and independent VLAN mode/version overrides; see [IGMP policy and inheritance](guides/igmp-snooping.md) |
| LAG | Existence, name, dynamic/static mode, and Ethernet membership |
| Ethernet | [Port name and administrative enable state](guides/ethernet.md), with drift recovery, import, replacement, deletion and reboot persistence verified |
| Protected ports | [Protected-port configuration and native query](guides/protected-ports.md) on Ethernet and populated LAG interfaces, with lifecycle and reboot persistence verified |
| Storm control | [Broadcast, multicast and unknown-unicast rate policies](guides/storm-control.md) on Ethernet and populated LAG interfaces; Ethernet and LAG lifecycles and reboot persistence verified |
| Jumbo frames | [Global configured mode and native query](guides/jumbo.md); configured lifecycle and reboot persistence verified; activation requires a saved configuration and reload; pending activation and cancellation verified through OpenTofu |
| DSCP trust | [Interface trust configuration and native query](guides/dscp-trust.md); Ethernet lifecycle, CLI drift repair, import, replacement, deletion and reboot persistence verified |
| Interface voice VLAN | [Local Ethernet IP voice VLAN policy](guides/voice-vlan.md), with native discovery, drift reconciliation, import, replacement and saved deletion |
| Routed VLAN interface | [VE existence, VLAN binding, port name and native configuration query](guides/ve.md); lifecycle, child guards and reboot persistence verified |
| Interface addresses | Individual IPv4/IPv6 addresses on VE and management interfaces |
| Spanning tree | Per-VLAN STP/RSTP mode and bridge priority; Ethernet admin-edge, BPDU guard and root guard |
| OSPF | Default-VRF area existence and interface bindings |
| Static routing | One IPv4 prefix and gateway relationship in the default VRF |
| DNS | Individual server addresses |
| Local users | Individual usernames, privileges and passwords |
| Global authentication | Authentication VLANs, global enablement, order, basic actions, MAC options, session limit and reauthentication |
| Interface authentication | Dot1x/MAC enablement and port-control on one Ethernet interface; global initialization is separate |
| AAA policy | Ordered login methods, default dot1x authentication, CoA enable and ignored actions |
| AAA servers | Individual RADIUS/TACACS server addresses, protocol ports, purposes and shared keys |
| LLDP | [Global and Ethernet enable state, MED policies, and native queries](guides/lldp.md), with CLI drift repair, import, replacement, omission, deletion and reboot persistence verified |
| PoE | Ethernet administrative enable state |
| Standard IPv4 ACLs | Numbered ACL existence and complete source-address rule sets |
| Extended IPv4 ACLs | Named or numbered ACLs with ordered prefixes, protocols, TCP/UDP ports and DSCP/priority markings |
| IPv6 ACLs | Named ACLs with prefixes, protocols, TCP/UDP ports, markings and syslog actions |
| MAC ACLs | Ordered MAC rules with arbitrary address masks, EtherType matches and logging; lifecycle, reboot, post-reboot updates and bound replacement verified |
| ACL discovery | Native identity inventory and ordered typed rule queries for standard IPv4, extended IPv4, IPv6 and MAC ACLs; populated, empty and missing-query behavior verified without configuration changes |
| ACL bindings | IPv4/IPv6 ingress and egress, and MAC ingress on Ethernet, LAG or whole VLANs |
| Persistence | Automatic saves or explicit configuration-save resource |

Data sources report active firmware, DNS servers, global and interface LLDP settings, PoE interface configuration and measurements, interface IP addresses, LAG configuration with Ethernet membership, IPv4 static routes, OSPF areas with interface bindings, VLAN/interface spanning-tree settings, [AAA policy, local-user privileges and RADIUS/TACACS server metadata](guides/aaa.md), and [global and per-port FlexAuth configuration](guides/authentication.md). [IGMP queries](guides/igmp-snooping.md) report native global settings and individual or all-VLAN overrides, including CLI-only overrides omitted by RESTCONF.

The [interface voice VLAN query](guides/voice-vlan.md) reports the configured local policy, including native-only settings; an absent policy returns null.

[Ethernet queries](guides/ethernet.md) report individual or all physical ports, native-derived descriptions and enable state, operational status, link metadata and unsigned 64-bit counters. Inventory and selected counters were checked against serial displays. RESTCONF link configuration metadata can remain stale after unsuccessful speed resets; it does not prove native automatic speed.

Configuration resources currently use RESTCONF. SSH is also required for firmware discovery, native configuration verification, persistence, and parent-deletion checks. SSH configuration fallback is not yet available. Configure RESTCONF and its configuration synchronization on the switch before using these resources.

A failed configuration request remains an error even if readback shows that the switch applied it. The failed resource operation stops further writes and skips automatic saving. Resources retain observed state where available and use `persistence_pending` to request reconciliation or saving on the next apply or destroy. A retry can save already converged configuration without repeating the completed mutation.

## Limits

- IGMP resources own global and VLAN querier mode and version. The tested RESTCONF API cannot reliably configure explicit per-VLAN disabling: its disabled value can select passive mode instead. Per-port versions, multicast group tables and other multicast controls are outside these resources. Queries report configured policy rather than effective forwarding.

- ACL support covers numbered standard and named or numbered extended IPv4 definitions, IPv6 and MAC definitions and Ethernet/LAG/VLAN access-group bindings; see [ACL ownership and limits](guides/acls.md). IPv4 logging and VLAN port subsets are not implemented. Use VLAN targets for whole-VLAN filtering, including routed VLANs.

- Extended AAA authentication services and keyless TACACS are not yet supported; see [AAA ownership and secret handling](guides/aaa.md).

- Global FlexAuth guest-VLAN writes, voice action variants and additional timers are not yet supported; see [authentication ownership and limits](guides/authentication.md).
- Ethernet speed, duplex and clock settings are not yet managed. On the tested `09.0.10kT213` build, RESTCONF auto-negotiation updates and individual leaf deletions did not restore native automatic speed. Deleting the Ethernet container restored speed but also changed an unrelated DHCP-client setting, so it is unsuitable for narrowly owned resource cleanup.
- Management-VLAN selection is not exposed. On the tested router image, the documented RESTCONF management-VLAN paths returned HTTP 400 with `unknown resource`, and the native `management-vlan` command was rejected. The command reference limits this feature to switch images.
- Spanning tree currently manages per-VLAN STP/RSTP mode and priority, plus Ethernet admin-edge, BPDU guard and root guard; see [spanning-tree ownership and limits](guides/spanning-tree.md).
- OSPF currently manages area existence and interface bindings; see [OSPF ownership and limits](guides/ospf.md).
- Static routes currently support IPv4 gateways in the default VRF; see [route ownership and limits](guides/routes.md).
- VE administrative enable state is not currently managed.
- DNS IPv4 succeeded on the tested build; its endpoint rejected IPv6 DNS addresses.
- PoE tests used a disconnected port. Reported power measurements under load have not been validated.
- Interface voice VLAN validation covered reboot persistence, a no-change plan after reload, and post-reboot update and deletion with unrelated configuration preserved. Phone connectivity and LLDP-MED negotiation were not tested.
- Reboot persistence was verified for populated and empty standard IPv4, extended IPv4, IPv6 and MAC ACLs, and IPv4 VLAN, IPv6 Ethernet and MAC Ethernet/LAG bindings, including unchanged native configuration and a no-change OpenTofu plan after reload. MAC checks also covered post-reboot updates and bound name replacement. This does not constitute exhaustive reboot validation of every resource.

Each resource owns only its documented settings. VLAN and VE deletion reject remaining child configuration. Avoid declaring the same remote object in multiple resources or states. Provider operations are serialized per hostname within one provider process; separate states, processes, DNS aliases, and external configuration changes require operator coordination.
