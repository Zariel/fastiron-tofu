# Access lists

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

The resource owns the complete ACL rule set. Import only ACLs whose rules this resource can represent. Existing remarks, logging, mirroring, noncontiguous wildcard masks, or other unsupported rule settings produce an error before mutation. Named standard ACLs and extended IPv4, IPv6 and MAC ACL definitions are not yet implemented. Access-group resources can bind existing ACLs of each family.

Defining an ACL does not attach it to an interface. Existing bindings remain unchanged during rule updates. Remove references to the ACL before destroying it; deletion checks interface access groups, multicast filters, access classes and route-map references. Changing `name` replaces the resource.

Import an existing numbered standard ACL using its native identity:

```sh
tofu import fastiron_ip_access_list_standard.sources 'ip access-list standard 90'
```

## Updates and persistence

RESTCONF writes require SSH access for native configuration verification and saving. The provider verifies the resulting rules and checks that unrelated configuration is unchanged before persistence.

FastIron requires removing a filter before changing it. Updates delete changed or removed sequences before adding replacements, so an attached ACL can briefly enforce an intermediate rule set. A failed operation can leave partial changes; inspect the diagnostic and reapply to converge. `persistence_pending` records that reconciliation or saving needs a retry. A failed save after deletion retains resource state until a retry can save the ACL's absence.

Omitting every `rule` manages an empty ACL. The tested firmware cannot create an empty ACL directly through RESTCONF, so creation briefly installs an owned `deny any` rule, removes it, and then saves the empty ACL. Removing the last rule from an existing ACL also leaves an empty ACL; destroying the resource removes the ACL itself.

Hardware workflows on the [tested firmware](../compatibility.md) covered creation, rule changes and sequence moves, import, drift correction, bound-deletion refusal, empty ACL transitions, name replacement, recreation and deletion. Running and startup configuration were checked independently over serial, including preservation of neighboring ACLs and bindings.

A reboot workflow verified a standard ACL with non-default rule sequences, an empty standard ACL, an IPv4 VLAN binding, an IPv6 Ethernet binding and a MAC LAG binding. Running and saved configuration remained unchanged after reload, and `tofu plan` reported no changes. Packet-filtering behavior has not yet been tested.

The [example](../../examples/acl/main.tf) includes provider configuration.

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

`interface` is a canonical Ethernet, LAG or VLAN name, such as `ethernet 1/1/9`, `lag 1` or `vlan 100`. `direction` defaults to `in`. The ACL and target LAG or VLAN must exist when the binding is applied; use resource references when creating them in the same apply. Extended IPv4, IPv6 and MAC ACL definitions currently need to be created outside this provider.

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
