# Authentication

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

This is separate from [AAA login, default dot1x, and CoA policy](aaa.md). `fastiron_authentication` provides a global resource and data source. Guest-VLAN writes, voice action variants, and additional timers remain unsupported by the global resource. Hardware workflows verified enablement, repeated mode changes, import, disablement, destroy and recreation, with independent running/startup checks and neighboring-port preservation. Client authentication exchanges and reboot persistence were not tested.

## Configure global authentication

Set `allow_aaa_changes = true` on the provider. Manage global initialization separately from port settings:

```hcl
resource "fastiron_vlan" "authentication" {
  vlan_id = 100
  name    = "AUTHENTICATION"
}

resource "fastiron_authentication" "switch" {
  auth_default_vlan          = fastiron_vlan.authentication.vlan_id
  dot1x_enabled              = true
  mac_authentication_enabled = true
}
```

The global resource owns `auth_default_vlan`, `restricted_vlan`, `critical_vlan`, `voice_vlan`, `auth_order`, `max_sessions`, `re_authentication`, `dot1x_enabled`, `mac_authentication_enabled`, `mac_dot1x_disable`, `mac_dot1x_override`, `failure_action`, and `timeout_action`.

Optional VLANs and actions are unconfigured when omitted. Authentication order defaults to `dot1x mac-auth`, `max_sessions` defaults to 2, and boolean settings default to false. Every referenced VLAN must already exist. Global enablement requires `auth_default_vlan`. `failure_action = "restricted-vlan"` requires `restricted_vlan`; `timeout_action = "critical-vlan"` requires `critical_vlan`. Other timeout actions are `success` and `failure`.

Guest VLAN and additional timers, including `reauth-period`, remain outside this resource's ownership and are preserved. The tested firmware's guest-VLAN DELETE can clear the REST view while leaving native configuration unchanged, so guest-VLAN writes are not exposed here. Existing global action variants with `voice voice-vlan` prevent global writes; their RESTCONF lifecycle is not yet supported.

Destroy resets owned settings to their defaults. Disabling a global authentication feature is rejected while a port still has that feature enabled, because FastIron would remove its port configuration. Declare the interface resource's dependency on the global resource so OpenTofu disables ports before resetting global settings.

Changed actions are cleared before removing their VLAN prerequisites and applied after the remaining settings. These transitions may briefly use the switch's default action. After a failure, `persistence_pending` records that reconciliation or saving is incomplete. Retry the apply: unchanged native action text does not prove a failed action application succeeded, so desired actions are reapplied before saving. Writes are verified against native configuration, and unrelated changes stop the operation before persistence.

Import the global configuration with:

```sh
tofu import fastiron_authentication.switch authentication
```

## Configure one interface

`fastiron_authentication_interface` owns dot1x enablement, MAC authentication enablement, and port-control on one Ethernet interface:

```hcl
resource "fastiron_authentication_interface" "access" {
  interface                  = "ethernet 1/1/9"
  dot1x_enabled              = true
  mac_authentication_enabled = true
  port_control               = "auto"

  depends_on = [fastiron_authentication.switch]
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

## Read global settings

```hcl
data "fastiron_authentication" "switch" {}

output "authentication_order" {
  value = data.fastiron_authentication.switch.auth_order
}
```

This data source reads native global configuration without modifying or saving it. It requires SSH credentials and host trust, but does not require `allow_aaa_changes`.

| Attribute | Meaning |
|---|---|
| `dot1x_enabled`, `mac_authentication_enabled` | Global feature enablement, independent of port enablement |
| `auth_order` | `dot1x mac-auth` (default) or `mac-auth dot1x` |
| `auth_default_vlan`, `restricted_vlan`, `critical_vlan`, `voice_vlan`, `guest_vlan` | Configured VLAN IDs; null when unconfigured |
| `max_sessions` | Global session limit, default 2 |
| `re_authentication` | Periodic reauthentication enabled; does not report the separately configured period |
| `mac_dot1x_disable` | Skip dot1x after successful MAC authentication with MAC-first order |
| `mac_dot1x_override` | Attempt dot1x after failed MAC authentication with MAC-first order and restricted-VLAN failure handling |
| `failure_action` | Configured `auth-fail-action` arguments, such as `restricted-vlan` |
| `timeout_action` | Configured `auth-timeout-action` arguments: `success`, `failure`, or `critical-vlan` |

Both action attributes are null when no action command is configured. This preserves the distinction between an explicit timeout `failure` action and the switch's unconfigured default. If configured, the native `voice voice-vlan` suffix is included in the action string. These values describe configuration; they do not verify successful client authentication.
