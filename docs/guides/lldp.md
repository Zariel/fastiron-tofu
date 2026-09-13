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

Read configured LLDP-MED policies without taking ownership:

```hcl
data "fastiron_lldp_med_policies" "switch" {}

output "med_policies" {
  value = data.fastiron_lldp_med_policies.switch.policies
}
```

Each policy contains `interface`, `application`, `traffic`, `vlan_id`, `priority`, and `dscp`. Results are sorted by interface and application. `vlan_id` is null unless traffic is tagged; `priority` is null for untagged traffic. An empty list means no native policies are configured. The query uses native configuration because the RESTCONF MED response can be stale, even reporting `[null]` while policies exist.

The MED query was verified through OpenTofu on `09.0.10kT213` with grouped policies, a per-port tagging change, CLI removal, and stable plans. Queries left running and saved configuration unchanged. These are configured policies, not evidence of endpoint advertisement or negotiation.

On this tested firmware, configuring a tagged or priority-tagged MED policy with priority `0` through CLI or RESTCONF produces native untagged configuration. The query reports that observed untagged mode, with null VLAN and priority, while retaining DSCP. Nonzero tagging priorities were preserved in the captures.

Manage one application policy on one Ethernet port:

```hcl
resource "fastiron_lldp_med_policy" "voice" {
  interface   = "ethernet 1/1/11"
  application = "voice"
  traffic     = "tagged"
  vlan_id     = 3053
  priority    = 3
  dscp        = 46
}
```

Tagged policies require `vlan_id` and `priority`. Priority-tagged policies require `priority` and omit `vlan_id`. Untagged policies omit both fields. Omitting `dscp` resets it to zero. Changing `interface` or `application` replaces the resource. Import with `tofu import fastiron_lldp_med_policy.voice 'lldp-med|ethernet 1/1/11|voice'`.

Updates remove cached references for the owned application and port before applying the new policy, temporarily leaving that policy absent. Every write verifies native state and preservation of other policies before continuing or saving. Deletion removes the policy without restoring a prior value. Failed operations retain `persistence_pending = true` through refresh so a retry can reconcile and save, including when native deletion has already completed.

The resource reports nonconvergence if firmware changes the requested tagging mode, including the priority-zero behavior described above; it does not silently substitute untagged configuration. Automated OpenTofu tests cover import, replacement, omission, partial deletion, and failed-save recovery. Full hardware resource lifecycle validation is in progress; backend mutation and read-only query checks have passed on the tested firmware.

Neighbor discovery is not yet implemented.
