# IPv4 static routes

`fastiron_ip_route` owns one destination prefix and IPv4 gateway in the default VRF. Multiple resources can manage different gateways for the same prefix.

The [complete static-route example](../../examples/static-route/main.tf) includes provider configuration, a managed route and both native queries.

```hcl
resource "fastiron_ip_route" "network" {
  prefix   = "198.51.100.0/24"
  next_hop = "192.0.2.2"
  distance = 200
}

data "fastiron_static_routes" "configured" {}
```

Use canonical network prefixes: `198.51.100.0/24`, rather than a host address with a network mask. `distance` is the administrative distance, defaults to 1, and accepts 1–255. FastIron treats 255 as unusable for routing.

Changing the prefix, gateway, or distance replaces this route relationship. The tested RESTCONF implementation cannot update an existing next hop's distance. Replacement temporarily removes that next hop; other next hops for the prefix remain configured.

Import by prefix and gateway:

```sh
tofu import fastiron_ip_route.network '198.51.100.0/24|192.0.2.2'
```

The data source returns a `routes` map keyed by the same identity, with `prefix`, `next_hop`, and `distance` for each entry. It reads configured routes, regardless of whether they are active in the forwarding table.

Use `fastiron_static_route` to select one prefix and gateway:

```hcl
data "fastiron_static_route" "network" {
  prefix   = fastiron_ip_route.network.prefix
  next_hop = fastiron_ip_route.network.next_hop

  depends_on = [fastiron_ip_route.network]
}
```

It returns those selectors, the canonical `id` and native `distance`, without writing or saving. A missing native route is an error even if RESTCONF retains its entry.

The single-route query has been verified through OpenTofu on FastIron `09.0.10kT213` against an unsaved CLI-created route with an independently owned name. The output matched native configuration, the plan was empty, and serial snapshots confirmed that the query neither changed nor saved configuration.

Refresh, queries and mutation readback use parsed native running configuration for route existence and distance. RESTCONF responses are still validated, but cached entries do not override native configuration. Both RESTCONF and SSH access are required. On tested FastIron `09.0.10kT213`, RESTCONF retained an old distance after a CLI route change; native observations expose that drift. An accepted RESTCONF write that does not produce the intended native route is an error and is not saved.

Deletion refuses to erase additional native route options, such as a name, tag, or BFD setting. Remove those options before destroying the route. IPv6 routes, interface or null next hops, non-default VRFs, and additional native options are not yet supported; discovery reports an error for route representations it cannot safely interpret.

After a route mutation, the provider verifies that unrelated native configuration remains unchanged, including neighboring routes' names and tags. A failed check prevents saving; it does not roll back changes already applied to running configuration.

Create, import, CLI distance drift recovery, distance replacement, omitted-distance defaults, guarded deletion and cleanup have been exercised through OpenTofu on FastIron `09.0.10kT213`. Independent serial checks verified running and saved configuration, including preservation of a neighboring next hop's CLI-only name and unrelated switch settings. Import did not change configuration; refusing deletion of a tagged route neither changed nor saved it.

Reboot persistence passed with identical running and saved configuration, matching native queries and an empty plan. Post-reboot replacement restored the default distance of 1. Deletion succeeded after removing the independently owned tag, and final cleanup restored the original running and saved configuration exactly.
