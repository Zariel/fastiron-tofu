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

data "fastiron_lldp_interfaces" "configured" {
  depends_on = [fastiron_lldp_interface.port]
}
```

The data source exposes an `interfaces` map from canonical interface names to configured enable state. A port's configured state is independent of whether LLDP is globally enabled.

Import global state with `tofu import fastiron_lldp.global lldp`, or a port with `tofu import fastiron_lldp_interface.port 'lldp|ethernet 1/1/12'`.

