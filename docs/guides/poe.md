# Power over Ethernet

`fastiron_interface_poe` owns administrative PoE enable state, priority and power allocation on one Ethernet interface. Ethernet administrative state and VLAN membership remain separately owned. Use the member’s canonical Ethernet name for a LAG member; native global port policies are supported.

| Argument | Default | Accepted values |
| --- | --- | --- |
| `enabled` | `true` | Administrative power enable state. |
| `priority` | `3` | 1 (highest) through 3 (lowest). |
| `power_by_class` | `0` | Allocation class 0–4; must be zero with an explicit limit. |
| `power_limit_milliwatts` | `0` | Zero for class-based allocation, or 1000–95000 subject to the port's capabilities. |

Omission and destroy restore these defaults. Disabling PoE clears priority and allocation on FastIron, so `enabled = false` requires default values for the other arguments. Unsupported power limits are rejected by the switch. Power policy changes can affect connected devices.

```hcl
resource "fastiron_interface_poe" "port" {
  interface             = "ethernet 1/1/12"
  priority              = 1
  power_limit_milliwatts = 18000
}

data "fastiron_poe_interfaces" "ports" {
  depends_on = [fastiron_interface_poe.port]
}
```

The provider writes the complete policy and verifies native configuration and preservation of unrelated commands before saving. Failed writes remain errors even if readback matches the requested policy; retrying a converged policy can finish persistence without repeating the power change.

Import with `tofu import fastiron_interface_poe.port 'poe|ethernet 1/1/12'`.

The data source's `interfaces` map includes only interfaces that expose PoE. Each value includes:

| Field | Meaning |
| --- | --- |
| `enabled` | Configured administrative enable state. |
| `priority` | Configured power priority: 1 (highest) to 3 (lowest, default). |
| `power_by_class` | Configured allocation class, 0–4; zero when an explicit power limit is configured. |
| `power_limit_milliwatts` | Configured limit; zero means class-based allocation. |
| `power_class` | Reported class of the connected powered device; null when unavailable. |
| `power_allocated_milliwatts` | Reported allocated power; null when unavailable. May lag configuration changes. |
| `power_used_milliwatts` | Reported consumption; null when unavailable. |

Configured policy comes from native configuration, including global port commands used for LAG members; cached RESTCONF configuration values can be stale. Unsupported or ambiguous native port settings produce an error instead of an inferred default. Missing operational measurements are null. Measurements do not participate in resource reconciliation.

Power measurements under load have not yet been hardware-validated.
