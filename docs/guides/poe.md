# Power over Ethernet

`fastiron_interface_poe` owns administrative PoE enable state on one Ethernet interface. Omission and destroy restore `enabled = true`. It does not own power priority, allocation class, limits, or Ethernet administrative state. Enable changes on ports with explicit allocation or priority settings are rejected because native enable changes can clear those settings.

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

The data source's `interfaces` map includes only interfaces that expose PoE. Each value includes:

| Field | Meaning |
| --- | --- |
| `enabled` | Configured administrative enable state. |
| `priority` | Configured power priority: 1 (highest) to 3 (lowest, default). |
| `power_by_class` | Configured allocation class, 0–4; zero when an explicit power limit is configured. |
| `power_limit_milliwatts` | Configured limit; zero means class-based allocation. |
| `power_class` | Reported class of the connected powered device; null when unavailable. |
| `power_used_milliwatts` | Reported consumption; null when unavailable. |

Configured policy comes from native configuration, including global port commands used for LAG members; cached RESTCONF configuration values can be stale. Unsupported or ambiguous native port settings produce an error instead of an inferred default. Missing operational measurements are null. Measurements do not participate in resource reconciliation.

Power measurements under load have not yet been hardware-validated.
