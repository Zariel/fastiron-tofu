# IGMP snooping

`fastiron_igmp_snooping` owns the switch's global IGMP snooping mode and version. Declare one per switch:

```hcl
resource "fastiron_igmp_snooping" "global" {
  querier_mode = "active"
  version      = 3
}
```

Global `querier_mode` accepts `active`, `passive` or `disabled`, defaulting to `disabled`. Global `version` accepts `2` or `3`, defaulting to `2`. Deleting the resource restores these defaults and preserves VLAN overrides and other multicast settings, including timers. Disabled mode clears explicit global snooping enablement; it does not remove independently configured VLAN policies.

Import with `tofu import fastiron_igmp_snooping.global global`. Match the imported settings in HCL to retain them; omitted global attributes select their defaults.

`fastiron_vlan_igmp_snooping` owns the IGMP querier mode and version overrides on an existing VLAN. VLAN existence and other multicast settings have separate ownership.

```hcl
resource "fastiron_vlan" "media" {
  vlan_id = 53
  name    = "MEDIA"
}

resource "fastiron_vlan_igmp_snooping" "media" {
  vlan_id      = fastiron_vlan.media.vlan_id
  querier_mode = "passive"
  version      = 3
}
```

`querier_mode` accepts `active` or `passive`. `version` accepts `2` or `3`. Omit either attribute to inherit that setting from the global configuration. Omitting both attributes leaves both settings inherited; it does not remove the VLAN. Deleting the resource restores inheritance while preserving other multicast settings, such as tracking, and leaves the VLAN in place.

Import with `tofu import fastiron_vlan_igmp_snooping.media 'vlan 53'`. Match the imported overrides in HCL to retain them. An omitted field plans removal of that override. Native changes are detected through SSH configuration reads, because RESTCONF can retain stale values or omit overrides configured through the CLI. Writes use RESTCONF and are independently verified before applying the provider's persistence policy.

A failed operation can leave `persistence_pending = true` with the observed native settings. Reapply to finish reconciliation or saving. The provider may briefly reset a changed override while replacing its RESTCONF entry; unchanged fields and separately owned configuration are preserved. Resetting a native-only disable override briefly uses passive mode.

The tested RESTCONF API can map `querier-mode: disabled` to passive mode. The global resource disables explicit global enablement by removing its configured mode. The VLAN resource does not offer explicit disabling: refresh can report `disabled` when a native disable override exists; choose a supported mode or omit the field to reset it. Per-port versions, multicast group tables and other snooping controls are not exposed by these resources.

The global data source reads configured mode and version without taking ownership or saving configuration:

```hcl
data "fastiron_igmp_snooping" "global" {}

output "global_igmp" {
  value = data.fastiron_igmp_snooping.global
}
```

It reports `disabled` when no explicit global mode is configured and `2` when no global version is configured. These are global settings; the query does not calculate effective VLAN or port policy.

The VLAN data source reads configured overrides:

```hcl
data "fastiron_vlan_igmp_snooping" "existing" {
  vlan_id = 53
}

output "igmp_overrides" {
  value = data.fastiron_vlan_igmp_snooping.existing
}
```

Its `querier_mode` and `version` are null when inherited. It reports native disable overrides even when RESTCONF omits them, and reports an error if the VLAN is absent. Hardware checks covered inherited, disabled and passive modes, missing VLANs and no-change plans; running and saved configuration remained unchanged by every query.

To discover all configured VLANs and their overrides:

```hcl
data "fastiron_igmp_snooping_vlans" "switch" {}

output "vlan_igmp" {
  value = data.fastiron_igmp_snooping_vlans.switch.vlans
}
```

The `vlans` map uses decimal VLAN IDs as keys. Each entry contains `vlan_id`, `querier_mode` and `version`. It includes the default VLAN, VLANs without overrides and CLI-only overrides that RESTCONF omits. Deleted native VLANs are excluded even if RESTCONF retains their entries. These queries report configured settings; they do not report effective multicast forwarding or group membership.

Hardware query checks covered global defaults, active/version 3, passive/version 2 and disabled/version 3, with inherited VLANs and a CLI-only disabled/version-3 VLAN override. Collection refresh removed a deleted VLAN. Every query preserved running and startup configuration, and cleanup restored the original configuration exactly.

See [tested compatibility](../compatibility.md) for the firmware used during hardware validation.

VLAN hardware validation covered creation, CLI drift correction, independent and combined inheritance, import without configuration changes, reset of native-only overrides, no-change plans and deletion. Tracking and unrelated configuration were preserved, and cleanup restored the original running and startup configuration exactly.

Combined global and VLAN checks covered global mode/version changes, CLI drift correction and read-only import. Global active/version 3, an inherited VLAN, an independent passive/version-2 VLAN override and separately configured multicast timers survived reboot. Running and startup configuration matched their pre-reboot snapshots exactly, and the post-reboot plan reported no changes. Global default reset and deletion preserved the independent VLAN override and timers in both running and saved configuration, with no-change plans afterward.
