# Link aggregation groups

`fastiron_lag` owns an aggregate's existence, name, mode, and complete Ethernet member set:

```hcl
resource "fastiron_lag" "storage" {
  lag_id  = 5
  name    = "storage"
  mode    = "dynamic"
  members = ["ethernet 1/1/7", "ethernet 1/1/8"]
}
```

Use `dynamic` for LACP or `static` for a static aggregate. Changing `mode` or `lag_id` requires replacement; renaming updates the existing aggregate. An explicit empty `members` set creates an empty aggregate. Import with `tofu import fastiron_lag.storage 'lag 5'`.

The LAG name above identifies the aggregate; it is not an interface description.

The [complete LAG example](../../examples/lag/main.tf) includes provider configuration, aggregate membership, interface policy and both queries.

Members must satisfy FastIron's LAG formation rules, including matching speeds and compatible port attributes. Remove independent VLAN memberships and routed configuration before adding a port. The provider rejects members already belonging to another LAG.

Removing a member or deleting a LAG disables its detached Ethernet ports, following FastIron's native behavior. This also applies during LAG replacement. The provider does not restore prior port settings. A separately managed Ethernet resource can re-enable a port on a subsequent apply; do not assume this happens within the same apply that detaches it.

On tested FastIron `09.0.10kT213`, removing a member through CLI can leave its old aggregate reference in RESTCONF. Refresh detects the native removal, but RESTCONF may acknowledge reattachment without changing native membership. The provider reports failed convergence and does not save; repeated applies can encounter the same limit. Restore the intended membership through CLI, then retry apply to reconcile and persist it. Reattaching a disabled port does not restore its previous administrative state.

Direct hardware checks confirmed that both PUT and PATCH of the cached aggregate reference could return success without attaching the port. Clearing the cached reference with an empty value returned an error. The provider does not use that failing write as a repair step.

Deleting a LAG through CLI on tested FastIron `09.0.10kT213` copies its STP protection flags onto the detached Ethernet ports. Those settings become independent port policy; removing the former LAG policy must not clear them. RESTCONF can retain the deleted aggregate entry, so interface policy resources check native LAG existence.

If saving fails, `persistence_pending` remains true so a subsequent apply can finish saving the observed configuration.

Remove VLAN relationships and independent interface or protocol settings before destroying the LAG. Deletion rejects remaining independent child configuration. Member-name and member-disable clauses do not block removal: names survive detach and detached members remain disabled.

## Interface settings

`fastiron_interface_lag` owns the virtual interface's description and administrative state. It does not create the aggregate or manage its membership:

```hcl
resource "fastiron_interface_lag" "storage" {
  lag_id    = fastiron_lag.storage.lag_id
  port_name = "Storage network"
  enabled   = true

  lifecycle {
    replace_triggered_by = [fastiron_lag.storage.id]
  }
}
```

The lifecycle rule replaces the interface policy when its parent is replaced, including mode changes that retain the same numeric ID. A numeric `lag_id` reference alone does not establish that replacement behavior. Parent renaming retains the interface policy. Changing `lag_id` replaces the policy and resets the old interface first.

`port_name` defaults to an empty string and `enabled` defaults to true. Omission and destruction restore those defaults. Non-default settings require at least one member; RESTCONF can acknowledge writes to an empty aggregate without creating native interface settings.

An administrative transition enables or disables all members using FastIron's native behavior. Previous member states are not restored. When the virtual interface is already enabled, description changes and no-change applies preserve independently disabled members. Individual member names, STP policy, VLAN relationships and other interface configuration remain independently owned.

Import with `tofu import fastiron_interface_lag.storage 'lag 5'`. Import and refresh read native configuration without writing or saving. If the parent disappears, refresh removes ordinary policy state without clearing settings left on detached ports. A failed deletion remains addressable while `persistence_pending` is true so another apply can finish saving.

Failed updates can retry persistence without repeating configuration writes when native settings already match. OpenTofu can taint a failed creation and replace that policy on retry.

On FastIron `09.0.10kT213`, hardware validation covered combined aggregate and interface creation, CLI description drift repair, administrative transitions, import, omitted defaults, forced policy replacement, parent mode replacement and parent identity replacement. Independent serial checks verified running and saved configuration, individual member names and unrelated settings. The replaced aggregate and interface policy survived reboot with identical running and saved configuration, matching queries and an empty OpenTofu plan. Independently disabled members remained disabled while the virtual interface was enabled.

Post-reboot policy updates and deletion have also passed native and saved-state checks, matching queries and an empty plan. Deletion cleared the interface description and enabled all members while preserving the aggregate, membership and individual member names. Subsequent parent deletion completed after RESTCONF member references synchronized; final cleanup restored the original running and saved configuration exactly.

Description resets use an empty PATCH value. On the tested firmware, deleting the description leaf directly timed out and blocked further RESTCONF reads and CLI interface configuration. The empty PATCH avoids that failing operation and has passed the post-reboot reset workflow. The synchronization-stall recovery described below requires serial access.

## Discovery

`fastiron_lags` reports native aggregate names, modes and Ethernet membership, checked against the RESTCONF physical-interface inventory. Stale cached aggregates are excluded, and incomplete member ranges produce a diagnostic rather than truncated membership.

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

`fastiron_lag` selects one aggregate by its numeric identity:

```hcl
data "fastiron_lag" "storage" {
  lag_id     = fastiron_lag.storage.lag_id
  depends_on = [fastiron_lag.storage]
}
```

It returns `lag_id`, canonical `id`, configured aggregate `name`, `mode` and the complete `members` set. An empty aggregate has an empty member set; a missing native aggregate is an error, even if RESTCONF retains its entry. Both aggregate queries use native configuration and validate membership against the physical-interface inventory without writing or saving.

The single-aggregate query has been verified against an existing dynamic LAG on FastIron `09.0.10kT213`, including exact member names and an empty plan. Independent serial snapshots confirmed that running and saved configuration remained unchanged.

Use `fastiron_interface_lag` to read a single aggregate's native interface description and administrative state:

```hcl
data "fastiron_interface_lag" "storage" {
  lag_id = fastiron_lag.storage.lag_id
}
```

It returns `lag_id`, canonical `name`, `port_name` and `enabled`. An omitted interface description is an empty string. `enabled` describes the virtual interface: individual members can remain disabled while it is true. Member names and the aggregate's configured LAG name are separate settings. The query requires RESTCONF and SSH access, performs no writes or saves, and reports an error if the native LAG is missing even when RESTCONF retains its entry.

On tested FastIron `09.0.10kT213`, query validation covers individual disabled members, interface description and disable state, CLI default resets, empty plans and parent deletion. Independent serial checks confirmed unchanged running and saved configuration across queries.

Immediately after an external LAG change, RESTCONF may briefly report a member referencing a deleted LAG. Discovery waits for this inconsistency to clear within `operation_timeout`. If synchronization does not finish, discovery reports an error; allow the switch to synchronize, then rerun the plan.

On tested FastIron `09.0.10kT213`, a deleted aggregate can also remain in RESTCONF without any member references. Recreating that identity through POST returns 409; PATCH may acknowledge the request without creating the native LAG, and DELETE may return 404 without clearing the cache. The provider reports this condition before attempting creation. Restore the parent through CLI before retrying.

Deleting a cached parent after external CLI removal and STP reference cleanup has also timed out, leaving RESTCONF synchronization in progress and blocking CLI interface configuration. The provider does not attempt this cache-deletion workaround for an absent native LAG.

Recovery from that synchronization stall was verified by reloading from startup configuration over the serial console. This discards unsaved running changes; verify the saved configuration before using it for recovery. The RESTCONF reboot request also timed out during the stall.

A CLI-restored LAG with disabled members and separately managed STP flags has passed reboot persistence checks: running and saved configuration remained identical, queries matched and OpenTofu reported an empty plan.

Replacement from static to dynamic mode with a separately managed STP policy has also passed through OpenTofu using RESTCONF. The policy referenced the LAG resource ID; both resources were replaced, protection flags were reapplied and detached members remained disabled. Native and saved configuration, both queries and an empty plan were verified after replacement. Subsequent destruction removed both resources successfully.

STP interface entries still reference their parent when all protection flags are false. Leaving such an entry behind can make LAG deletion return RESTCONF 404. STP resource deletion resets its flags and removes that entry, verifying native configuration and RESTCONF absence. FastIron can rebuild the reference afterward, so LAG deletion also clears it immediately before deleting the parent, after confirming that no independent native interface configuration remains. Direct checks verified back-to-back reference and parent deletion using the provider’s request headers.

If DELETE reports absence while a native LAG remains, the provider reports the failure without saving. Remove dependent policy first. CLI removal followed by another apply can finish persistence and state cleanup when RESTCONF deletion remains unavailable.

## VLAN membership

Use `fastiron_vlan_membership` to manage individual tagged or untagged relationships on an existing LAG:

```hcl
resource "fastiron_vlan" "storage" {
  vlan_id = 53
  name    = "storage"
}

resource "fastiron_vlan_membership" "storage_lag" {
  vlan_id   = fastiron_vlan.storage.vlan_id
  interface = fastiron_lag.storage.id
  tagging   = "tagged"
}
```

Import this relationship with `tofu import fastiron_vlan_membership.storage_lag 'vlan 53|lag 5|tagged'`. Removing it preserves other VLAN memberships on the LAG. Removing an untagged relationship restores the default VLAN. Configure VLAN membership on the aggregate rather than separately on its Ethernet members.
