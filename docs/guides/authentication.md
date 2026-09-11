# Interface authentication discovery

`fastiron_authentication_interfaces` reads configured FlexAuth settings for Ethernet interfaces:

```hcl
data "fastiron_authentication_interfaces" "switch" {}

output "authentication_interfaces" {
  value = data.fastiron_authentication_interfaces.switch.interfaces
}
```

`interfaces` is a map keyed by canonical names such as `ethernet 1/1/9`. Each entry contains:

| Attribute | Meaning |
|---|---|
| `dot1x_enabled` | Dot1x is configured on this port |
| `mac_authentication_enabled` | MAC authentication is configured on this port |
| `port_control` | `auto`, `force-authorized`, or `force-unauthorized` |

The map includes interfaces mentioned in dot1x enablement, MAC authentication enablement, or explicit port-control configuration. It is empty when none are configured. An omitted native port-control setting is reported as its default, `force-authorized`.

The data source reads native configuration over SSH. On the tested firmware, changing a port's control mode can leave it listed under both its old and new modes in RESTCONF. Native configuration identifies the current mode. Conflicting native modes or unsupported port expressions produce an error instead of partial results.

Discovery requires the provider's SSH credentials and host trust configuration. It does not require `allow_aaa_changes` and does not modify or save switch configuration. These are configuration flags: global feature initialization, AAA policy, VLAN requirements, and connected clients determine whether authentication actually takes place.

This is separate from [AAA login, default dot1x, and CoA policy](aaa.md). FlexAuth global settings and per-port write resources remain under development. Hardware discovery was checked for dot1x/MAC enablement, mode changes, neighboring ports, and cleanup; client authentication exchanges were not tested.
