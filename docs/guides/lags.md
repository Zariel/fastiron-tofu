# Read LAG configuration

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

Immediately after an external LAG change, RESTCONF may briefly report a member referencing a deleted LAG. Discovery reports an error until the switch synchronizes its interface configuration. Allow synchronization to finish, then rerun the plan.

## VLAN membership

Use `fastiron_vlan_membership` to manage individual tagged or untagged relationships on an existing LAG:

```hcl
resource "fastiron_vlan" "storage" {
  vlan_id = 53
  name    = "storage"
}

resource "fastiron_vlan_membership" "storage_lag" {
  vlan_id   = fastiron_vlan.storage.vlan_id
  interface = "lag 5"
  tagging   = "tagged"
}
```

Import this relationship with `tofu import fastiron_vlan_membership.storage_lag 'vlan 53|lag 5|tagged'`. Removing it preserves other VLAN memberships on the LAG. Removing an untagged relationship restores the default VLAN. Configure VLAN membership on the aggregate rather than separately on its Ethernet members.
