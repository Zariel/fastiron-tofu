# Storm control

`fastiron_interface_storm_control` owns all broadcast, multicast and unknown-unicast rate limits on one Ethernet or populated LAG interface. FastIron requires these classes to share one unit.

```hcl
resource "fastiron_interface_storm_control" "edge" {
  interface             = "ethernet 1/1/12"
  unit                  = "kbps"
  broadcast_limit       = 1000
  multicast_limit       = 2000
  unknown_unicast_limit = 1000
}
```

Set `unit` to `kbps` or `pps` and configure at least one limit. Omitting a class removes its limit. The documented input ranges are 1–1,000,000 kbps and 1–8,388,607 pps; a device may impose narrower limits. On tested firmware `09.0.10kT213`, the broadcast limit accepted 2 pps but rejected 1 pps through both RESTCONF and the native CLI.

Changing units temporarily removes the complete policy before applying limits in the new unit. Deleting the resource removes all three limits. Changing `interface` replaces the resource and removes the policy on the old interface. No prior policy is restored. Interface settings, VLANs and LAG membership remain separately owned.

Use `interface = fastiron_lag.example.id` for an aggregate. A LAG must have members and expose its interface. Individual LAG members are rejected; configure the aggregate instead. Declare only one storm-control resource per interface.

Import an existing policy with its canonical interface name:

```sh
tofu import fastiron_interface_storm_control.edge 'ethernet 1/1/12'
```

RESTCONF rate writes can silently remove native logging, threshold and shutdown actions. The resource rejects policies with these options before mutation or adoption. Remove those options separately before managing the policy. Global logging timers remain outside resource ownership.

Read configured rates without taking ownership:

```hcl
data "fastiron_interface_storm_control" "edge" {
  interface = "ethernet 1/1/12"
}

output "broadcast_limit" {
  value = data.fastiron_interface_storm_control.edge.broadcast_limit
}
```

The query returns null for disabled classes and a null `unit` when the policy is absent. `has_native_options` indicates logging, threshold or shutdown options; it does not describe their values. A missing parent interface is an error. Queries neither mutate nor save configuration and report configured rates, not measured traffic enforcement.

Reads use native configuration through SSH because RESTCONF metadata can retain old rates or omit CLI-created policies. Writes use RESTCONF and verify native results and unrelated configuration before saving. A failed operation retains observed state and `persistence_pending` so a later apply or destroy can retry reconciliation or persistence.

Direct hardware probes covered Ethernet and LAG updates, class deletion, unit changes and stale metadata repair. Full OpenTofu lifecycle and reboot validation for this feature are still in progress.
