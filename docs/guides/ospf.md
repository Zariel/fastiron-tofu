# OSPF areas and interface bindings

`fastiron_router_ospf_area` owns an OSPF area in the default VRF. `fastiron_router_ospf_interface` independently owns one interface's binding to an area.

```hcl
resource "fastiron_router_ospf_area" "transit" {
  area_id = "0.0.0.53"
}

resource "fastiron_router_ospf_interface" "transit" {
  area_id   = fastiron_router_ospf_area.transit.area_id
  interface = "ve 53"
}

data "fastiron_ospf_areas" "configured" {}
```

Use dotted area identifiers, including `0.0.0.0` for the backbone. The provider normalizes decimal area identifiers returned by FastIron, so changing how the switch represents an identifier does not create drift.

Reads, imports, drift detection, and write verification use native running configuration. FastIron's RESTCONF cache can omit a newly configured area or retain outdated bindings. RESTCONF supplies write paths and capability checks, but its cached contents do not establish native presence or absence. A successful REST response without the requested native change fails reconciliation and is not saved.

Create the interface and its IP configuration before binding it to OSPF. Interface names use native forms such as `ve 53`, `ethernet 1/1/3`, or `lag 5`; availability depends on the firmware and interface configuration. Changing an area ID or binding replaces that resource. An interface already bound to another area must be unbound first.

Area deletion requires all interface bindings and additional native area options to be removed first. Binding deletion also refuses to erase additional OSPF interface options, such as a configured network type. Other areas and their bindings remain independently managed. Removing the last managed area does not remove the OSPF process.

Import existing configuration:

```sh
tofu import fastiron_router_ospf_area.transit '0.0.0.53'
tofu import fastiron_router_ospf_interface.transit '0.0.0.53|ve 53'
```

After importing, run `tofu apply` with the area reference retained so OpenTofu records the dependency before removing the configuration. If an area deletion encounters a remaining binding, remove the binding and apply again.

The data source returns an `areas` map keyed by dotted area ID. Each entry contains an `interfaces` set. It reports configuration, not neighbor adjacencies or learned routes.

The current RESTCONF implementation manages area existence and interface bindings. Process settings, stub/NSSA options, redistribution, interface timers, authentication, and network type are not yet managed. Hardware tests on FastIron `09.0.10kT213` use a VE interface; Ethernet and LAG binding compatibility has not yet been tested.
