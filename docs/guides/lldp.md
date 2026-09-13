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

Global resource writes use SSH to verify native configuration and preservation of unowned settings before saving. A mismatch between RESTCONF and native state fails the operation; it does not permit an unverified save.

On tested firmware, a port reports `enabled = true` when either receive or transmit is enabled. Writing `true` to an already enabled port preserves a receive-only or transmit-only mode. Disabling and then re-enabling the port restores both directions; the RESTCONF boolean does not independently manage those directions.
