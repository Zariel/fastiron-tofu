# Routed VLAN interfaces

`fastiron_interface_ve` owns VE existence and its port name. The parent VLAN must exist, and `ve_id` and `vlan_id` must match the FastIron routed VLAN identity. Omission of `port_name` clears the description.

```hcl
resource "fastiron_vlan" "transit" {
  vlan_id = 3053
}

resource "fastiron_interface_ve" "transit" {
  ve_id     = 3053
  vlan_id   = fastiron_vlan.transit.vlan_id
  port_name = "TRANSIT"
}
```

Import with `tofu import fastiron_interface_ve.transit 've 3053'`. The resource exposes the canonical `name`, for example `ve 3053`, for independently managed address and protocol resources.

Reads require SSH access to native configuration as well as RESTCONF. Native configuration determines VE existence and its port name because RESTCONF can retain an old name or interface after CLI changes. Missing, duplicated or inconsistent RESTCONF interface metadata is rejected rather than treated as confirmed configuration.

Use the data source to inspect an existing VE without taking ownership:

```hcl
data "fastiron_interface_ve" "transit" {
  ve_id = 3053
}

output "transit_enabled" {
  value = data.fastiron_interface_ve.transit.enabled
}
```

It returns `name`, `vlan_id`, `port_name` and native administrative `enabled` state. A missing VE is an error. The query does not change or save configuration, and administrative enablement does not establish link or routing readiness.

Destroy checks native configuration and refuses to remove a VE with addresses, protocol bindings, or other child settings. Remove those children first. The VLAN resource independently guards against deletion while a VE still exists.

VE administrative enable state is not currently supported by this resource. On tested FastIron `09.0.10kT213`, RESTCONF PUT and PATCH acknowledged and echoed `enabled` changes without changing native administrative configuration. Native `disable` remains an independently owned child and blocks resource deletion until removed.
