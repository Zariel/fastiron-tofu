# Jumbo frames

The `fastiron_jumbo` resource owns configured global jumbo-frame support. Declare one per switch:

```hcl
resource "fastiron_jumbo" "switch" {
  enabled = true
}
```

Jumbo mode changes require saving the configuration and reloading the switch to take effect. Reloads are initiated separately from provider operations. `active_enabled` reports the switch’s active mode; `reload_required` is true when running configuration differs from that mode. Save before reloading, especially when using manual persistence.

Deleting the resource configures jumbo mode off; the active mode remains until the saved configuration is loaded by a reload. Use the data source to inspect activation status after deletion. Per-interface MTUs remain independently owned. Import existing configuration with `tofu import fastiron_jumbo.switch global`.

The data source reads the configured mode without taking ownership:

```hcl
data "fastiron_jumbo" "switch" {}

output "jumbo_enabled" {
  value = data.fastiron_jumbo.switch.enabled
}
```

This query requires an available RESTCONF jumbo endpoint and SSH access to native configuration. It does not change or save configuration. The RESTCONF configured flag can lag behind CLI changes, so `enabled` comes from the native global `jumbo` command. `active_enabled` comes from the operational flag and changes when saved configuration is loaded during a reload. The query also returns `reload_required`; it does not verify whether running configuration has been saved.

Jumbo mode is global, not an interface setting. The query reports configured support; it does not measure forwarding, set per-interface MTUs, or establish a supported maximum frame size. FastIron excludes the out-of-band management port and switch-access protocols from jumbo mode.

The tested firmware rejected RESTCONF DELETE with HTTP 501, so removal writes `enabled = false`. Before changing the mode, the provider waits for RESTCONF configuration to synchronize with native state; otherwise an acknowledged request can be skipped. It then verifies native convergence and preservation of unrelated configuration before saving. Synchronization and convergence retries are bounded by the configured RESTCONF timeout. Each in-flight read retains its own transport deadline, and post-write verification has a fresh retry budget.

Failed operations retain observed state and `persistence_pending` so a later apply or destroy can retry reconciliation or saving. Setting the already configured native value avoids an unnecessary RESTCONF write.

Direct hardware probes covered repeated transitions and CLI drift repair in both directions, restoring the original configuration. The public OpenTofu lifecycle verified defaults, drift repair, native-only removal, import, replacement, configured-state persistence across reboot, and post-reboot updates and deletion. Live activation-status checks verified the disabled baseline, a saved enable awaiting reload, and cancellation before reload, including stable OpenTofu plans. Reboot observations verified that the operational flag follows the saved mode loaded at startup; automated tests cover pending changes in both directions and refresh after an external reload. The query reports configured support, not measured packet forwarding.
