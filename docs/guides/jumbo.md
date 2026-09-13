# Jumbo frames

The `fastiron_jumbo` data source reads configured global jumbo-frame support:

```hcl
data "fastiron_jumbo" "switch" {}

output "jumbo_enabled" {
  value = data.fastiron_jumbo.switch.enabled
}
```

This query requires an available RESTCONF jumbo endpoint and SSH access to native configuration. It does not change or save configuration. Both RESTCONF flags can remain stale after CLI changes, so the reported value comes from the native global `jumbo` command.

Jumbo mode is global, not an interface setting. The query reports configured support; it does not measure forwarding, set per-interface MTUs, or establish a supported maximum frame size. FastIron excludes the out-of-band management port and switch-access protocols from jumbo mode.

A configuration resource is not available yet. Write reconciliation and hardware lifecycle validation are in progress. The tested firmware rejected RESTCONF DELETE with HTTP 501; a successful write response alone did not always establish native convergence.
