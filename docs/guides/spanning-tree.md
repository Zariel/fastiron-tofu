# Spanning tree

`fastiron_spanning_tree_vlan` owns the spanning-tree mode and bridge priority on one existing VLAN. Creating the resource enables spanning tree; destroying it disables spanning tree on that VLAN.

The VLAN ID may be 1–4095, including a relocated default VLAN. ID 4095 is reserved for the active default; this resource does not create the VLAN or change the default selection.

```hcl
resource "fastiron_spanning_tree_vlan" "servers" {
  vlan_id  = fastiron_vlan.servers.vlan_id
  mode     = "rstp"
  priority = 4096
}

data "fastiron_spanning_tree" "configured" {
  depends_on = [fastiron_spanning_tree_vlan.servers]
}
```

Modes are `stp` (classic 802.1D) and `rstp` (802.1w). FastIron exposes per-VLAN RSTP through its RESTCONF `rapid-pvst` container. Bridge priority defaults to 32768 and accepts 0–65535, including values that are not multiples of 4096.

Priority changes update in place. Mode changes replace the resource, temporarily disabling spanning tree on the target VLAN. Import existing configuration before changing its mode:

```sh
tofu import fastiron_spanning_tree_vlan.servers 53
```

The data source returns a `vlans` map keyed by VLAN ID, with native `mode` and `priority` for each enabled configuration, including the default VLAN. CLI-only settings are included and stale RESTCONF entries are excluded. Unsupported native spanning-tree settings produce a diagnostic instead of potentially incorrect defaults. Managing spanning-tree settings does not create or delete the VLAN itself.

Use `depends_on` when the query must follow resource changes. After removing a resource, run `tofu apply -refresh-only` if you need stored query outputs to reflect its deletion: OpenTofu may have evaluated the query before destruction.

Its `interfaces` map reports configured `admin_edge`, `bpdu_guard` and `root_guard` options, keyed by canonical interface name (for example, `ethernet 1/1/12`). Interfaces without explicit native STP flags are omitted. Flag values and interface identities come from native configuration, including settings absent from RESTCONF, so rebuilding the RESTCONF cache after a reboot does not change the reported configuration. These values describe configuration, not operational protection or forwarding state. An ignored RESTCONF update returns an error and is not saved.

On the tested firmware, removing RSTP leaves classic STP enabled. Resource deletion waits for RESTCONF and native configuration to agree, then removes the remaining classic entry and verifies native absence. Other VLANs retain their settings. If deletion fails partway through, a subsequent apply resumes from the observed configuration.

Before saving, VLAN writes verify native policy and check that unrelated configuration is preserved, including the target VLAN's name, membership and existence. A detected unrelated change returns an error without saving.

Deletion refuses to erase additional native spanning-tree settings, such as timers or per-VLAN port costs. Remove those settings before destroying or replacing this resource.

`fastiron_spanning_tree_interface` owns three options on an existing Ethernet or LAG interface:

```hcl
resource "fastiron_spanning_tree_interface" "server" {
  interface  = "ethernet 1/1/12"
  admin_edge = true
  bpdu_guard = true
  root_guard = false
}
```

All three options default to false. Omitting an option resets it to false; destroying the resource resets all three. Port names, administrative enable state, VLAN membership and other STP options remain separately managed. Changing `interface` replaces the resource, resetting the old port before configuring the new one. Import uses the canonical interface name:

```sh
tofu import fastiron_spanning_tree_interface.server 'ethernet 1/1/12'
```

For a LAG, set `interface = fastiron_lag.uplink.id` or use a canonical name such as `lag 11`. Configure these options on the aggregate: both primary and secondary Ethernet members are rejected because RESTCONF can acknowledge member writes without changing native STP policy. The data source reports the LAG's explicit native flags and excludes cache-only member entries.

Interface updates verify native configuration before saving, including preservation of unrelated commands. After CLI changes, the provider may first synchronize RESTCONF with the current native flags before applying the desired flags. If synchronization times out or a write fails, the operation reports an error without saving; a later apply can resume reconciliation.

These flags do not enable spanning tree on a VLAN. Configure the corresponding VLAN's spanning-tree mode separately for the protection to operate. `admin_edge` configures the native RSTP edge-port option; `root_guard` configures root protection.

Global spanning-tree mode, MST, timers, path costs and port priorities are not yet managed by these resources. Hardware validation uses FastIron `09.0.10kT213`. Ethernet interface flags have been verified through creation, individual updates, omitted defaults, CLI drift repair, import, replacement, deletion and reboot.

On that firmware, the `/stp/global`, `/stp/rstp` and `/stp/mstp` RESTCONF containers return “unknown resource”. Per-VLAN RSTP is available through `/stp/rapid-pvst`.

VLAN updates wait for RESTCONF and native configuration to agree before writing: a priority update matching stale RESTCONF state can otherwise be ignored, and stale presence can reject recreation. Synchronization is bounded by the configured RESTCONF timeout; a timeout or ignored update returns an error without saving, and a later apply can retry.

On non-default VLANs, validation covers creation, default and boundary priorities, classic STP and RSTP priority drift repair, import, mode replacement, recreation after external deletion, replacement onto another VLAN and deletion. RSTP configuration survived reboot with unchanged running and saved configuration and an empty plan afterward. Relocated default VLAN 4095 has also passed import, priority updates and default reset, replacement from classic STP to RSTP, deletion and a fresh query after deletion; unrelated configuration was preserved.

LAG interface flags have passed creation, import, individual updates, omitted defaults, CLI drift repair, member rejection and deletion, with native and saved configuration checked independently. LAG-specific reboot and recovery after external parent deletion are not yet validated.
