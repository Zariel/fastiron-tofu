# Power over Ethernet

`fastiron_interface_poe` owns administrative PoE enable state on one Ethernet interface. Omission and destroy restore `enabled = true`. It does not own power priority, limits, or Ethernet administrative state.

```hcl
resource "fastiron_interface_poe" "port" {
  interface = "ethernet 1/1/12"
  enabled   = false
}

data "fastiron_poe_interfaces" "ports" {
  depends_on = [fastiron_interface_poe.port]
}
```

Import with `tofu import fastiron_interface_poe.port 'poe|ethernet 1/1/12'`.

The data source's `interfaces` map includes only interfaces that expose PoE. Each value has configured `enabled`, reported `power_class`, and `power_used_milliwatts`. Missing operational measurements are null. Measurements do not participate in resource reconciliation.

Power measurements under load have not yet been hardware-validated.
