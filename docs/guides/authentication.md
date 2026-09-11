# Interface authentication

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

This is separate from [AAA login, default dot1x, and CoA policy](aaa.md). FlexAuth global settings remain under development. Hardware workflows verified enablement, repeated mode changes, import, disablement, destroy and recreation, with independent running/startup checks and neighboring-port preservation. Client authentication exchanges and reboot persistence were not tested.

## Configure one interface

`fastiron_authentication_interface` owns dot1x enablement, MAC authentication enablement, and port-control on one Ethernet interface:

```hcl
resource "fastiron_authentication_interface" "access" {
  interface                  = "ethernet 1/1/9"
  dot1x_enabled              = true
  mac_authentication_enabled = true
  port_control               = "auto"
}
```

Set `allow_aaa_changes = true` on the provider. Configure the global authentication VLAN and initialize each enabled authentication feature before applying this resource. The resource preserves those global settings; it does not create them. Remove any explicit untagged VLAN membership from the port first. FastIron manages the port's implicit default-VLAN membership as authentication is enabled or disabled. Tagged memberships and other ports remain outside this resource's ownership.

After changing port enablement, the provider reapplies the existing global `auth-fail-action restricted-vlan` and `auth-timeout-action success`, `failure`, or `critical-vlan` settings. A failed reapplication is retried even if the port flags already match; switch configuration is saved only after reconciliation succeeds. Global action variants with `voice voice-vlan` still block enablement changes. Port-control-only updates preserve those variants.

Both enablement flags default to `false`. `port_control` defaults to `force-authorized`, which permits traffic without authenticating clients. `auto` selects authentication-controlled access; `force-unauthorized` denies access. Both require `dot1x_enabled = true`. Disabling dot1x resets its control mode to `force-authorized`; MAC authentication enablement is independent. Destroy disables both authentication types and returns control to `force-authorized`, without restoring an earlier configuration.

Mode updates can briefly return the port to `force-authorized` while clearing a stale mode entry. A failed update can leave that intermediate mode in place; inspect the diagnostic and reapply to reconcile it. `persistence_pending` records incomplete reconciliation or saving. With `persistence_mode = "after_each_write"`, the provider saves only after native configuration converges and unrelated configuration has been verified.

Changing `interface` replaces the resource. Import an existing port using its canonical name:

```sh
tofu import fastiron_authentication_interface.access 'ethernet 1/1/9'
```
