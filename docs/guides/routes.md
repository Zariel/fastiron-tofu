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

Refresh, queries and mutation readback use parsed native running configuration for route existence and distance. RESTCONF responses are still validated, but cached entries do not override native configuration. Both RESTCONF and SSH access are required. On tested FastIron `09.0.10kT213`, RESTCONF retained an old distance after a CLI route change; native observations expose that drift. An accepted RESTCONF write that does not produce the intended native route is an error and is not saved.

Deletion refuses to erase additional native route options, such as a name, tag, or BFD setting. Remove those options before destroying the route. IPv6 routes, interface or null next hops, non-default VRFs, and additional native options are not yet supported; discovery reports an error for route representations it cannot safely interpret.

Create, import, no-change planning, distance replacement, and deletion have been exercised on FastIron `09.0.10kT213`. Running and saved configuration were checked over serial, including preservation of another next hop for the same prefix. Reboot persistence has not yet been verified.
