# Per-VLAN spanning tree

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

The data source returns a `vlans` map keyed by VLAN ID, with `mode` and `priority` for each enabled configuration. It includes the default VLAN. Managing spanning-tree settings does not create or delete the VLAN itself.

On the tested firmware, removing RSTP leaves classic STP enabled. Resource deletion waits for RESTCONF and native configuration to agree, then removes the remaining classic entry and verifies native absence. Other VLANs retain their settings. If deletion fails partway through, a subsequent apply resumes from the observed configuration.

Deletion refuses to erase additional native spanning-tree settings, such as timers or per-VLAN port costs. Remove those settings before destroying or replacing this resource.

Global spanning-tree mode, MST, timers, and per-interface settings are not yet managed by these resources. Hardware validation uses FastIron `09.0.10kT213`; reboot persistence has not yet been verified.
