# DSCP trust

`fastiron_interface_dscp_trust` owns whether an interface is configured to honor Layer 3 DSCP-based QoS. The native default honors Layer 2 CoS instead.

```hcl
resource "fastiron_interface_dscp_trust" "edge" {
  interface = "ethernet 1/1/12"
  enabled   = true
}
```

Setting `enabled = false` or deleting the resource disables DSCP trust. Changing `interface` replaces the resource and disables trust on the old interface. The resource does not restore prior settings or own port priority, QoS mappings, flow control, interface settings or LAG membership.

RESTCONF operations were tested on Ethernet interfaces with FastIron `09.0.10kT213`. Although native LAG configuration supports `trust dscp`, its corresponding RESTCONF endpoint returned 404 for reads and 400 for writes on this firmware. Availability is checked on the device without a firmware or model allowlist. Individual LAG members are rejected because interface-level configuration belongs to their aggregate. SSH configuration fallback is not yet available.

Import with the canonical interface name:

```sh
tofu import fastiron_interface_dscp_trust.edge 'ethernet 1/1/12'
```

Read configured trust without taking ownership:

```hcl
data "fastiron_interface_dscp_trust" "edge" {
  interface = "ethernet 1/1/12"
}

output "dscp_trust" {
  value = data.fastiron_interface_dscp_trust.edge.enabled
}
```

The query reports native configuration, including CLI changes that RESTCONF metadata has not reflected. It returns false for an existing interface without trust configured and fails for a missing parent or unavailable endpoint. Queries neither mutate nor save configuration. They do not measure packet classification or effective scheduling.

FastIron documents DSCP trust and global symmetrical flow control as incompatible: enabling trust can disable symmetrical flow control. The resource rejects operations involving enabled trust while that global setting is present. This includes removal when reconciliation needs to align an enabled native value first. Disable symmetrical flow control separately before reconciling such a policy; the provider does not take ownership of it. Read-only queries remain available.

The command reference also marks 802.1p priority override as incompatible. Ordinary untagged port priority was preserved during enable/disable testing; that does not establish support for priority overrides or measure traffic behavior.

The tested RESTCONF DELETE operation returned HTTP 501. Removal therefore writes `enabled = false`. Writes align the current native value before applying the desired value, verify native convergence and preserve unrelated configuration before saving. Failed operations retain observed state and `persistence_pending` for a later apply or destroy to retry.

Direct hardware probes verified enable/disable, CLI drift, current-value alignment and priority preservation, with original configuration restored. Full OpenTofu lifecycle and reboot validation are in progress.
