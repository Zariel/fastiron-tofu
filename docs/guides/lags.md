# Link aggregation groups

`fastiron_lag` owns an aggregate's existence, name, mode, and complete Ethernet member set:

```hcl
resource "fastiron_lag" "storage" {
  lag_id  = 5
  name    = "storage"
  mode    = "dynamic"
  members = ["ethernet 1/1/7", "ethernet 1/1/8"]
}
```

Use `dynamic` for LACP or `static` for a static aggregate. Changing `mode` or `lag_id` requires replacement; renaming updates the existing aggregate. An explicit empty `members` set creates an empty aggregate. Import with `tofu import fastiron_lag.storage 'lag 5'`.

Members must satisfy FastIron's LAG formation rules, including matching speeds and compatible port attributes. Remove independent VLAN memberships and routed configuration before adding a port. The provider rejects members already belonging to another LAG.

Removing a member or deleting a LAG disables its detached Ethernet ports, following FastIron's native behavior. This also applies during LAG replacement. The provider does not restore prior port settings. A separately managed Ethernet resource can re-enable a port on a subsequent apply; do not assume this happens within the same apply that detaches it.

If saving fails, `persistence_pending` remains true so a subsequent apply can finish saving the observed configuration.

Remove VLAN relationships and independent interface or protocol settings before destroying the LAG. Deletion rejects remaining child configuration.

## Discovery

`fastiron_lags` reports configured link aggregation groups and their Ethernet members through RESTCONF.

```hcl
data "fastiron_lags" "switch" {}

output "lags" {
  value = data.fastiron_lags.switch.lags
}
```

The `lags` map is keyed by canonical interface name, such as `lag 1`. Each entry contains:

- `lag_id`: numeric identity.
- `name`: configured LAG name, such as `uplink`.
- `mode`: `dynamic` for LACP or `static`.
- `members`: a set of canonical Ethernet names, such as `ethernet 1/2/1`.

Membership describes configuration, including disconnected members. It does not indicate link health or whether LACP has formed a working aggregate.

Immediately after an external LAG change, RESTCONF may briefly report a member referencing a deleted LAG. Discovery waits for this inconsistency to clear within `operation_timeout`. If synchronization does not finish, discovery reports an error; allow the switch to synchronize, then rerun the plan.

## VLAN membership

Use `fastiron_vlan_membership` to manage individual tagged or untagged relationships on an existing LAG:

```hcl
resource "fastiron_vlan" "storage" {
  vlan_id = 53
  name    = "storage"
}

resource "fastiron_vlan_membership" "storage_lag" {
  vlan_id   = fastiron_vlan.storage.vlan_id
  interface = fastiron_lag.storage.id
  tagging   = "tagged"
}
```

Import this relationship with `tofu import fastiron_vlan_membership.storage_lag 'vlan 53|lag 5|tagged'`. Removing it preserves other VLAN memberships on the LAG. Removing an untagged relationship restores the default VLAN. Configure VLAN membership on the aggregate rather than separately on its Ethernet members.
