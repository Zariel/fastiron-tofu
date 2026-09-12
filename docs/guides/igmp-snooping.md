# VLAN IGMP snooping

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

The tested RESTCONF API maps `querier-mode: disabled` to passive mode, so this resource does not offer explicit disabling. Refresh can report `disabled` when a native disable override exists; choose a supported mode or omit the field to reset it. Global IGMP configuration, per-port versions, multicast group tables and other snooping controls are not yet exposed.

See [tested compatibility](../compatibility.md) for the firmware used during hardware validation.

Hardware validation covered creation, CLI drift correction, independent and combined inheritance, import without configuration changes, reset of native-only overrides, no-change plans and deletion. Tracking and unrelated configuration were preserved, and cleanup restored the original running and startup configuration exactly. Reboot and global-policy interaction checks remain pending.
