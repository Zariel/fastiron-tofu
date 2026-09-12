# Access lists

## Standard IPv4 ACLs

`fastiron_ip_access_list_standard` manages one numbered IPv4 standard ACL and all its source-address rules:

```hcl
resource "fastiron_ip_access_list_standard" "sources" {
  name = "90"

  rule {
    sequence = 10
    action   = "permit"
    source   = "192.0.2.0/24"
  }

  rule {
    sequence = 20
    action   = "deny"
  }
}
```

`name` is a string containing a canonical number from `1` through `99`. Each `rule` has a distinct `sequence` from 1 through 65000 and an `action` of `permit` or `deny`. Rules are evaluated in sequence order. `source` defaults to `any`; otherwise use a canonical IPv4 network prefix, including `/32` for a single host. Duplicate action/source combinations at different sequence numbers are rejected by the tested firmware.

The resource owns the complete ACL rule set. Import only ACLs whose rules this resource can represent. Existing remarks, logging, mirroring, noncontiguous wildcard masks, or other unsupported rule settings produce an error before mutation. Named standard ACLs, IPv6 and MAC ACL definitions are not implemented. Access-group resources can bind existing ACLs of each family.

Defining an ACL does not attach it to an interface. Existing bindings remain unchanged during rule updates. Remove references to the ACL before destroying it; deletion checks interface access groups, multicast filters, access classes and route-map references. Changing `name` replaces the resource.

Import an existing numbered standard ACL using its native identity:

```sh
tofu import fastiron_ip_access_list_standard.sources 'ip access-list standard 90'
```

## Extended IPv4 ACLs

`fastiron_ip_access_list_extended` owns one named or numbered extended IPv4 ACL and its complete rule set:

```hcl
resource "fastiron_ip_access_list_extended" "web" {
  name = "WEB"

  rule {
    sequence         = 10
    action           = "permit"
    protocol         = 6
    source           = "192.0.2.0/24"
    destination      = "198.51.100.10/32"
    destination_port = "443"
  }

  rule {
    sequence = 20
    action   = "deny"
  }
}
```

Use a name of up to 47 bytes or a canonical number from `100` through `199`. Each rule requires a distinct `sequence` from 1 through 65000 and an `action` of `permit` or `deny`.

| Rule field | Values and defaults |
|---|---|
| `source`, `destination` | Canonical IPv4 prefix, including `/32` for one host; default `any`. |
| `protocol` | Numeric IP protocol from 1 through 254, such as `6` for TCP or `17` for UDP. Omit for all protocols; IPv4 protocol zero is not a distinct filter. |
| `source_port`, `destination_port` | Port from 0 through 65535 as a canonical decimal string such as `"443"`, inclusive range such as `"3000..4000"`, or default `"any"`. Numeric matches require TCP or UDP. Port zero is an explicit match. |
| `dscp` | Match a DSCP value from 0 through 63. Omit to match any DSCP. |
| `dscp_marking` | Mark DSCP with a value from 0 through 63. Omit to leave it unchanged. |
| `internal_priority_marking` | Set internal priority from 0 through 7. Omit to leave it unchanged. |

Explicit zero markings are preserved. Use a single port number instead of a range with equal endpoints. Duplicate packet rules at different sequences are rejected.

Import uses the native identity:

```sh
tofu import fastiron_ip_access_list_extended.web 'ip access-list extended WEB'
```

The same ownership, binding, replacement and persistence rules apply to standard and extended ACL resources. Existing unsupported rule options prevent adoption. IPv4 logging and descriptions are not exposed because the tested RESTCONF implementation accepts these fields without applying them; TCP flags, ICMP type/code filters and noncontiguous wildcard masks are also outside the supported rule model.

## Updates and persistence

RESTCONF writes require SSH access for native configuration verification and saving. The provider verifies the resulting rules and checks that unrelated configuration is unchanged before persistence.

After changing an ACL through CLI, allow the switch to synchronize its RESTCONF database before applying. Extended ACL resources reject mismatched rule-sequence views before mutation. Request synchronization with `restconf config-sync` in global configuration mode, and check `show restconf config` until synchronization is no longer in progress. On the tested firmware, CLI parent deletion sometimes left stale RESTCONF records even after synchronization; disabling and re-enabling RESTCONF cleared them. The provider does not restart the management service automatically.

FastIron requires removing a filter before changing it. Updates delete changed or removed sequences before adding replacements, so an attached ACL can briefly enforce an intermediate rule set. A failed operation can leave partial changes; inspect the diagnostic and reapply to converge. `persistence_pending` records that reconciliation or saving needs a retry. A failed save after deletion retains resource state until a retry can save the ACL's absence.

Omitting every `rule` manages an empty ACL. The tested firmware cannot create an empty ACL directly through RESTCONF, so creation briefly installs an owned `deny any` rule, removes it, and then saves the empty ACL. Removing the last rule from an existing ACL also leaves an empty ACL; destroying the resource removes the ACL itself.

Standard IPv4 hardware workflows on the [tested firmware](../compatibility.md) covered creation, rule changes and sequence moves, import, drift correction, bound-deletion refusal, empty ACL transitions, name replacement, recreation and deletion. Running and startup configuration were checked independently over serial, including preservation of neighboring ACLs and bindings.

Extended IPv4 workflows covered empty and populated creation, import, port ranges and explicit zero markings, sequence and marking changes, recovery after synchronization, name replacement, empty transitions, recreation and deletion. Successful phases checked native running/startup configuration, neighboring configuration and a no-change OpenTofu plan.

Independent RESTCONF/serial checks covered every protocol number from 1 through 254, TCP and UDP ports from 0 through 1023, and ranges including `0..65535` and high ports. A provider workflow created 256 rules, grew to 257, removed the extra rule and deleted the ACL, checking every rule through serial and obtaining no-change plans after creation and updates. A separate reboot workflow preserved a named extended ACL with UDP ports and DSCP/priority markings and an empty numbered extended ACL. Native running/saved configuration remained unchanged, and the post-reboot plan reported no changes.

A reboot workflow verified a standard ACL with non-default rule sequences, an empty standard ACL, an IPv4 VLAN binding, an IPv6 Ethernet binding and a MAC LAG binding. Running and saved configuration remained unchanged after reload, and `tofu plan` reported no changes. Packet-filtering behavior has not yet been tested.

The [standard ACL example](../../examples/acl/main.tf) and [extended ACL example](../../examples/acl-extended/main.tf) include provider configuration.

## Bind ACLs to interfaces

Use an access-group resource to bind an existing ACL:

```hcl
resource "fastiron_ip_access_group" "sources" {
  interface = "ethernet 1/1/9"
  direction = "in"
  acl       = fastiron_ip_access_list_standard.sources.name
}
```

The reference to the ACL resource gives OpenTofu the dependency needed to create the ACL before binding it and remove the binding before destroying the ACL.

| Resource | ACL family | Directions |
|---|---|---|
| `fastiron_ip_access_group` | Standard or extended IPv4 | `in`, `out` |
| `fastiron_ipv6_access_group` | IPv6 | `in`, `out` |
| `fastiron_mac_access_group` | MAC | `in` |

`interface` is a canonical Ethernet, LAG or VLAN name, such as `ethernet 1/1/9`, `lag 1` or `vlan 100`. `direction` defaults to `in`. The ACL and target LAG or VLAN must exist when the binding is applied; use resource references when creating them in the same apply. IPv6 and MAC ACL definitions currently need to be created outside this provider.

Each resource owns one interface/family/direction slot. Declare only one resource for each slot. Other families, the opposite direction, other interfaces and ACL rules remain independently owned. Changing `acl` replaces the active binding in place. Changing `interface` or `direction` replaces the resource. Destroy removes the binding currently occupying the owned slot, including a binding changed outside OpenTofu.

Import identifies the slot without an ACL name:

```sh
tofu import fastiron_ip_access_group.sources 'ethernet 1/1/9 in'
tofu import fastiron_mac_access_group.lag 'lag 1 in'
```

The provider verifies native configuration and removes stale REST entries left behind by binding changes. `persistence_pending` indicates that reconciliation, stale-entry cleanup or saving needs a retry. Refresh retains pending cleanup after a failed deletion so the next apply can finish it.

Hardware workflows on the [tested firmware](../compatibility.md) covered all three binding families on Ethernet and LAG interfaces: creation with ACL dependencies, default direction, changes and changes back, import, external drift correction, interface/direction replacement, and deletion before ACL parents. Each phase checked running/startup configuration over serial, REST entries, neighboring bindings and a no-change plan.

VLAN bindings apply to the whole VLAN and do not require a VE. Direct REST/native checks covered creation and deletion for all supported families and directions. An OpenTofu workflow verified IPv4 VLAN binding creation with its ACL and VLAN parents, default direction, import, external drift correction, direction replacement, recreation after external VLAN deletion, and deletion before its parents. Each phase checked running/startup configuration over serial, REST entries, neighboring bindings, VLAN names and a no-change plan.

Bindings that contain additional native settings, including `logging enable` or a VLAN port subset, cannot be adopted by these resources. Use `vlan <id>` for whole-VLAN filtering, including routed VLANs; `ve <id>` is not an accepted resource target.
