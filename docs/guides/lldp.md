# LLDP

`fastiron_lldp` owns global enable state; `fastiron_lldp_interface` owns enable state on one Ethernet interface. Neither resource changes the other's scope. Omission and destroy restore `enabled = true`.

```hcl
resource "fastiron_lldp" "global" {
  enabled = true
}

resource "fastiron_lldp_interface" "port" {
  interface = "ethernet 1/1/12"
  enabled   = false
}

data "fastiron_lldp" "global" {
  depends_on = [fastiron_lldp.global]
}

data "fastiron_lldp_interfaces" "configured" {
  depends_on = [fastiron_lldp_interface.port]
}
```

`data.fastiron_lldp.global.enabled` reports global configuration. `fastiron_lldp_interfaces` exposes an `interfaces` map from canonical interface names to configured enable state. A port's configured state is independent of whether LLDP is globally enabled.

Import global state with `tofu import fastiron_lldp.global lldp`, or a port with `tofu import fastiron_lldp_interface.port 'lldp|ethernet 1/1/12'`.

Global and per-port resource and data-source reads use SSH for native configuration because RESTCONF can retain old values after CLI changes. Global writes synchronize a stale RESTCONF value to current native state before applying a change. Each write verifies native convergence and preservation of unowned settings before proceeding or saving. Port writes allow native range regrouping but verify that other ports retain their individual receive and transmit settings.

On tested firmware, a port reports `enabled = true` when either receive or transmit is enabled. An already enabled port satisfies `enabled = true` without a mutation, preserving a receive-only or transmit-only mode. Disabling and then re-enabling the port restores both directions; the RESTCONF boolean does not independently manage those directions.

Port writes also synchronize a stale RESTCONF value before applying a change. When disabling a one-way port whose cached value is already `false`, this briefly enables both directions on that port before disabling it. FastIron acknowledges direct PATCH and PUT requests for an already cached value without necessarily changing native configuration. A failed synchronization or mutation stops the operation without saving; refresh and retry reconcile the resulting native state.

Global enable configuration was verified through OpenTofu on FastIron `09.0.10kT213`: defaults, updates, CLI drift repair, import without configuration changes, replacement, omission, deletion, and persistence across reboot. The checks preserved an independently configured receive-only port mode and unrelated running and saved configuration. Automated recovery tests cover unsuccessful saves and partial failures during RESTCONF synchronization and mutation.

Ethernet enable configuration was also verified through OpenTofu on `09.0.10kT213`: defaults, disable, receive-only CLI drift repair, import without configuration changes, replacement, omission, deletion, and persistence across reboot. Native running and saved configuration matched exactly across reload, with a stable plan afterward. The workflow preserved another port's receive-only mode and unrelated configuration, then restored the original running and saved baseline. Automated tests cover native range regrouping, collateral direction changes, stale-cache synchronization, partial failures, and retries without repeating completed mutations.

LLDP-MED network policies and neighbor discovery are not yet implemented.
