# VLANs and discovery

`fastiron_vlan` manages VLAN existence and name for IDs 2 through 4094. Memberships, spanning tree, ACL bindings and routed interfaces have separate resources. Remove dependent configuration before deleting a VLAN.

The `fastiron_vlan` and `fastiron_vlans` data sources read IDs and names through RESTCONF without taking ownership or saving configuration. They can read the default VLAN, which the resource does not manage.

```hcl
data "fastiron_vlans" "switch" {}

data "fastiron_vlan" "selected" {
  vlan_id = 50
}

output "vlans" {
  value = data.fastiron_vlans.switch.vlans
}

output "selected_name" {
  value = data.fastiron_vlan.selected.name
}
```

The collection's `vlans` map uses decimal VLAN IDs as keys. Each value contains an integer `vlan_id` and a string `name`. An unnamed VLAN has an empty name. The individual query accepts IDs 1 through 4094 and reports a diagnostic when the requested VLAN is absent. Neither query includes membership, management/default VLAN selection, or other VLAN policies.

Hardware checks on the [tested firmware](../compatibility.md) covered default and unnamed VLANs, refresh after a name change, missing-VLAN diagnostics, collection refresh after deletion, and no-change plans. Serial checks verified that discovery left running and saved configuration unchanged.

See the [discovery example](../../examples/vlan-discovery/main.tf) for provider configuration.
