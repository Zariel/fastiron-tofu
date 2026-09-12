# VLANs and discovery

`fastiron_vlan` manages VLAN existence and name for IDs 2 through 4094. Memberships, spanning tree, ACL bindings and routed interfaces have separate resources. Remove dependent configuration before deleting a VLAN.

The ordinary VLAN resource refuses the currently selected default VLAN, even when its ID is not 1. The exact name `DEFAULT-VLAN` is reserved: FastIron interprets it as a request to move the global default VLAN, rather than a normal name change. Lowercase and mixed-case variants remain ordinary names.

`fastiron_default_vlan` owns the global default selection. Declare one per switch:

```hcl
resource "fastiron_default_vlan" "switch" {
  vlan_id = 4095
}
```

The ID may be 1 through 4095 and must be unused unless it is already the active default. FastIron renumbers the existing default VLAN and its associated VE, preserving their settings. Coordinate changes with resources or external configuration that refer to their former IDs. The ordinary VLAN resource still accepts only IDs 2 through 4094.

Import with `tofu import fastiron_default_vlan.switch default`. Read the current selection and match `vlan_id` in HCL before applying if you want to preserve it. Deleting this resource resets the default to VLAN 1; it does not restore a previously selected ID. If another VLAN occupies ID 1, release that ID before deletion. Failed cleanup or saving leaves `persistence_pending = true` so a subsequent apply can retry.

Selection uses RESTCONF writes and native configuration reads over SSH to verify the active default and preserve its configuration. Orphaned RESTCONF entries from former defaults are cleaned up without deleting native VLANs. Provider persistence settings apply to this resource.

Management-VLAN selection is also not exposed. Its documented RESTCONF paths and native command were unavailable on the tested router image; see [compatibility limits](../compatibility.md).

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

The collection's `vlans` map uses decimal VLAN IDs as keys. Each value contains an integer `vlan_id` and a string `name`. An unnamed VLAN has an empty name. The individual query accepts IDs 1 through 4095, including 4095 when used by the default VLAN, and reports a diagnostic when the requested VLAN is absent. Neither query includes membership, management/default VLAN selection, or other VLAN policies.

After a default-VLAN change, RESTCONF can temporarily list both the former and current default entries. Names alone therefore do not identify the active default. Ordinary VLAN write protection uses the native global setting.

Hardware checks on the [tested firmware](../compatibility.md) covered default and unnamed VLANs, refresh after a name change, missing-VLAN diagnostics, collection refresh after deletion, and no-change plans. Serial checks verified that discovery left running and saved configuration unchanged.

Default selection checks covered creation, updates including ID 4095, import, correction after a CLI change, reset to 1 and deletion. Serial checks confirmed saved configuration after changes and exact restoration of the original running and startup configuration after cleanup. Reboot validation for default selection remains pending.

See the [discovery example](../../examples/vlan-discovery/main.tf) and [default selection example](../../examples/default-vlan/main.tf) for provider configuration.
