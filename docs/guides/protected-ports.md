# Protected ports

`fastiron_interface_protected_port` owns protected-port isolation on one physical Ethernet or LAG interface:

```hcl
resource "fastiron_interface_protected_port" "guest" {
  interface = "ethernet 1/1/12"
  enabled   = true
}
```

FastIron isolates traffic between protected interfaces at the system level, with an exception for CPU-bound or CPU-originated traffic. Setting `enabled = false` or deleting the resource disables protection. The resource does not own the interface, its administrative enable state, VLAN membership or LAG membership, and it does not restore prior settings. Changing `interface` replaces the resource and disables protection on the old interface.

Use the canonical LAG name to protect an aggregate. The LAG must already have members and expose an interface; an empty LAG has no configurable interface on the tested firmware. FastIron assigns interface-level configuration to the aggregate, so the provider rejects protected-port resources and queries targeting individual LAG members. Use the LAG interface instead.

```hcl
resource "fastiron_interface_protected_port" "aggregate" {
  interface = fastiron_lag.guest.id
  enabled   = true
}
```

Import with the canonical interface name:

```sh
tofu import fastiron_interface_protected_port.guest 'ethernet 1/1/12'
```

Read native configuration without taking ownership:

```hcl
data "fastiron_interface_protected_port" "guest" {
  interface = "ethernet 1/1/12"
}

output "protected" {
  value = data.fastiron_interface_protected_port.guest.enabled
}
```

The query reports `false` for an existing unprotected interface and fails for a missing interface. It reports configuration, not measured packet isolation. Queries do not save configuration.

On tested firmware `09.0.10kT213`, RESTCONF protected-port metadata can retain a removed setting or omit one configured through the CLI. Reads therefore use native configuration through the provider's SSH read path. Writes use RESTCONF, verify native results and preserve unrelated settings. LAG drift recovery may invalidate cached metadata before reapplying protection; native-only removal may materialize the setting before deleting it. Failed saves retain `persistence_pending` so a subsequent apply or destroy can retry persistence.

Hardware validation covered Ethernet and LAG creation, CLI drift repair, native-only removal, import, replacement and deletion. Both interface kinds retained protection across reboot with identical running and saved configuration and a no-change OpenTofu plan. Post-reboot removal also passed, with unrelated interface and switch configuration preserved.

FastIron does not support protected ports on routed, VE, management or loopback interfaces, mirror/monitor ports, private VLANs, OpenFlow ports or MCT. Its configuration guide advises against protection on uplinks, DHCP server or trusted ports, active spanning-tree paths, and IGMP/MLD router or source ports. Plan isolation around the topology and use the switch's diagnostics for unsupported combinations.
