# Spanning tree

`fastiron_spanning_tree_vlan` owns the spanning-tree mode and bridge priority on one existing VLAN. Creating the resource enables spanning tree; destroying it disables spanning tree on that VLAN.

```hcl
resource "fastiron_spanning_tree_vlan" "servers" {
  vlan_id  = fastiron_vlan.servers.vlan_id
  mode     = "rstp"
  priority = 4096
}

data "fastiron_spanning_tree" "configured" {}
```

Modes are `stp` (classic 802.1D) and `rstp` (802.1w). FastIron exposes per-VLAN RSTP through its RESTCONF `rapid-pvst` container. Bridge priority defaults to 32768 and accepts 0–65535, including values that are not multiples of 4096.

Priority changes update in place. Mode changes replace the resource, temporarily disabling spanning tree on the target VLAN. Import existing configuration before changing its mode:

```sh
tofu import fastiron_spanning_tree_vlan.servers 53
```

The data source returns a `vlans` map keyed by VLAN ID, with native `mode` and `priority` for each enabled configuration, including the default VLAN. CLI-only settings are included and stale RESTCONF entries are excluded. Unsupported native spanning-tree settings produce a diagnostic instead of potentially incorrect defaults. Managing spanning-tree settings does not create or delete the VLAN itself.

Its `interfaces` map reports configured `admin_edge`, `bpdu_guard` and `root_guard` options, keyed by canonical interface name (for example, `ethernet 1/1/12`). Interfaces with no explicit STP options may be absent; explicitly disabled entries can remain in RESTCONF. Flag values come from native configuration, including settings absent from RESTCONF. Cached interface entries with no native flags report false defaults. These values describe configuration, not operational protection or forwarding state. An ignored RESTCONF update returns an error and is not saved.

On the tested firmware, removing RSTP leaves classic STP enabled. Resource deletion waits for RESTCONF and native configuration to agree, then removes the remaining classic entry and verifies native absence. Other VLANs retain their settings. If deletion fails partway through, a subsequent apply resumes from the observed configuration.

Deletion refuses to erase additional native spanning-tree settings, such as timers or per-VLAN port costs. Remove those settings before destroying or replacing this resource.

`fastiron_spanning_tree_interface` owns three options on an existing Ethernet interface:

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

Interface updates verify native configuration before saving, including preservation of unrelated commands. After CLI changes, the provider may first synchronize RESTCONF with the current native flags before applying the desired flags. If synchronization times out or a write fails, the operation reports an error without saving; a later apply can resume reconciliation.

These flags do not enable spanning tree on a VLAN. Configure the corresponding VLAN's spanning-tree mode separately for the protection to operate. `admin_edge` configures the native RSTP edge-port option; `root_guard` configures root protection.

Global spanning-tree mode, MST, timers, path costs and port priorities are not yet managed by these resources. Hardware validation uses FastIron `09.0.10kT213`; reboot persistence has not yet been verified.
