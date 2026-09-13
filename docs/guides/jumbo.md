# Jumbo frames

The `fastiron_jumbo` resource owns configured global jumbo-frame support. Declare one per switch:

```hcl
resource "fastiron_jumbo" "switch" {
  enabled = true
}
```

Deleting the resource disables jumbo mode. It does not restore a previous value or manage per-interface MTU configuration. Import existing configuration with `tofu import fastiron_jumbo.switch global`.

The data source reads the configured mode without taking ownership:

```hcl
data "fastiron_jumbo" "switch" {}

output "jumbo_enabled" {
  value = data.fastiron_jumbo.switch.enabled
}
```

This query requires an available RESTCONF jumbo endpoint and SSH access to native configuration. It does not change or save configuration. Both RESTCONF flags can remain stale after CLI changes, so the reported value comes from the native global `jumbo` command.

Jumbo mode is global, not an interface setting. The query reports configured support; it does not measure forwarding, set per-interface MTUs, or establish a supported maximum frame size. FastIron excludes the out-of-band management port and switch-access protocols from jumbo mode.

The tested firmware rejected RESTCONF DELETE with HTTP 501, so removal writes `enabled = false`. Before changing the mode, the provider waits for RESTCONF configuration to synchronize with native state; otherwise an acknowledged request can be skipped. It then verifies native convergence and preservation of unrelated configuration before saving. Synchronization and convergence are bounded by the configured RESTCONF timeout.

Failed operations retain observed state and `persistence_pending` so a later apply or destroy can retry reconciliation or saving. Setting the already configured native value avoids an unnecessary RESTCONF write.

Direct hardware probes covered repeated transitions and CLI drift repair in both directions, restoring the original configuration. Public OpenTofu lifecycle and reboot validation are in progress. The query reports configured support, not measured packet forwarding.
