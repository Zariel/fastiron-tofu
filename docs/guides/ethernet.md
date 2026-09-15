# Ethernet interfaces

`fastiron_interface_ethernet` manages a physical port's description and administrative enable state:

```hcl
resource "fastiron_interface_ethernet" "port" {
  port      = "1/1/12"
  port_name = "PHONE"
  enabled   = false
}
```

Omitting `port_name` clears the description. Omitting `enabled` enables the port. Deleting the resource does both; it does not delete or broadly reset the physical interface. VLAN membership, PoE, voice VLAN and other interface settings remain separately owned. Changing `port` replaces the resource and resets the old port's owned fields.

Import with the canonical interface name:

```sh
tofu import fastiron_interface_ethernet.port 'ethernet 1/1/12'
```

## Read-only discovery

Query one port or the physical Ethernet inventory without taking ownership:

```hcl
data "fastiron_interface_ethernet" "port" {
  port = "1/1/12"
}

data "fastiron_ethernet_interfaces" "all" {}

output "port_status" {
  value = data.fastiron_interface_ethernet.port.oper_status
}

output "received_bytes" {
  value = data.fastiron_interface_ethernet.port.counters["in-octets"]
}

output "interfaces" {
  value = data.fastiron_ethernet_interfaces.all.interfaces
}
```

The inventory map uses canonical names such as `ethernet 1/1/12` as keys and excludes logical interfaces. Each observation contains the port identity, description, enable state, interface index, administrative and operational status, counters and link metadata. Missing operational fields are `null`, not fabricated zero or false values. A single-port query fails if the port is absent.

Both the resource and queries read description and enable state from the native-derived RESTCONF `state` fields. On the tested firmware, the `config` fields can retain an old description after a CLI change following resource deletion. The resource detects that drift and can clear a restored description even when RESTCONF already caches the desired empty value.

On `09.0.10kT213`, hardware validation covered adoption, independent field resets, CLI drift repair, import, port replacement and deletion, including cached-default recovery. A named, disabled port survived reboot with identical running and saved configuration and a no-change OpenTofu plan. Resetting both fields and deleting the resource after reboot also passed; unrelated configuration was preserved.

Counters preserve the full unsigned 64-bit range as OpenTofu numbers. Counter keys retain the switch's names, including `in-octets`, `out-pkts` and `in-errors`. Counters and negotiated link observations can change between reads; those changes may update outputs but do not configure the port or become Ethernet resource attributes. Queries do not save configuration.

## Link metadata and configuration limits

The `link` object separates configuration metadata (`auto_negotiate`, `duplex`, `speed`, `clock`) from state reports (`reported_auto_negotiate`, `reported_duplex`) and negotiated values. Speed identities retain their reported names, such as `openconfig-if-ethernet:SPEED_100MB` and `openconfig-if-ethernet:SPEED_UNKNOWN`.

Link configuration metadata is a RESTCONF report, not proof of native speed configuration. On the tested `09.0.10kT213` firmware, a failed automatic-speed reset can leave that metadata disagreeing with native forced speed. Do not use `link.auto_negotiate` alone to establish that native speed has been reset.

Speed, duplex and clock writes are not exposed. Individual RESTCONF resets did not reliably restore automatic speed, while deleting the Ethernet container changed an unrelated DHCP-client setting. These operations require further transport support; the provider does not delete and reconstruct neighboring configuration to work around that boundary. Separate resources manage [protected-port isolation](protected-ports.md), [storm control](storm-control.md), [global jumbo mode](jumbo.md), and [DSCP trust](dscp-trust.md).
