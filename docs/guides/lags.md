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

Aggregate interface descriptions and administrative-state management are not yet implemented. The LAG name above identifies the aggregate; it is not an interface description.

Members must satisfy FastIron's LAG formation rules, including matching speeds and compatible port attributes. Remove independent VLAN memberships and routed configuration before adding a port. The provider rejects members already belonging to another LAG.

Removing a member or deleting a LAG disables its detached Ethernet ports, following FastIron's native behavior. This also applies during LAG replacement. The provider does not restore prior port settings. A separately managed Ethernet resource can re-enable a port on a subsequent apply; do not assume this happens within the same apply that detaches it.

Deleting a LAG through CLI on tested FastIron `09.0.10kT213` copies its STP protection flags onto the detached Ethernet ports. Those settings become independent port policy; removing the former LAG policy must not clear them. RESTCONF can retain the deleted aggregate entry, so interface policy resources check native LAG existence.

If saving fails, `persistence_pending` remains true so a subsequent apply can finish saving the observed configuration.

Remove VLAN relationships and independent interface or protocol settings before destroying the LAG. Deletion rejects remaining independent child configuration. Member-name and member-disable clauses do not block removal: names survive detach and detached members remain disabled.

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
