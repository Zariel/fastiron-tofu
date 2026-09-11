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

The resource owns the complete ACL rule set. Import only ACLs whose rules this resource can represent. Existing remarks, logging, mirroring, noncontiguous wildcard masks, or other unsupported rule settings produce an error before mutation. Named standard ACLs, extended IPv4, IPv6 and MAC ACLs, and ACL bindings are not yet implemented.

Defining an ACL does not attach it to an interface. Existing bindings remain unchanged during rule updates. Remove references to the ACL before destroying it; deletion checks interface access groups, multicast filters, access classes and route-map references. Changing `name` replaces the resource.

Import an existing numbered standard ACL using its native identity:

```sh
tofu import fastiron_ip_access_list_standard.sources 'ip access-list standard 90'
```

## Updates and persistence

RESTCONF writes require SSH access for native configuration verification and saving. The provider verifies the resulting rules and checks that unrelated configuration is unchanged before persistence.

FastIron requires removing a filter before changing it. Updates delete changed or removed sequences before adding replacements, so an attached ACL can briefly enforce an intermediate rule set. A failed operation can leave partial changes; inspect the diagnostic and reapply to converge. `persistence_pending` records that reconciliation or saving needs a retry. A failed save after deletion retains resource state until a retry can save the ACL's absence.

Omitting every `rule` manages an empty ACL. The tested firmware cannot create an empty ACL directly through RESTCONF, so creation briefly installs an owned `deny any` rule, removes it, and then saves the empty ACL. Removing the last rule from an existing ACL also leaves an empty ACL; destroying the resource removes the ACL itself.

Hardware workflows on the [tested firmware](../compatibility.md) covered creation, rule changes and sequence moves, import, drift correction, bound-deletion refusal, empty ACL transitions, name replacement, recreation and deletion. Running and startup configuration were checked independently over serial, including preservation of neighboring ACLs and bindings. Packet-filtering behavior and reboot persistence have not yet been tested.

The [example](../../examples/acl/main.tf) includes provider configuration.
