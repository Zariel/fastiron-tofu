# Interface addresses

IPv4 and IPv6 addresses are independently managed on VE and management interfaces. Each resource owns one host address and prefix; other addresses remain untouched.

```hcl
resource "fastiron_interface_ipv4_address" "transit" {
  interface = fastiron_interface_ve.transit.name
  address   = "192.0.2.1/30"
}

resource "fastiron_interface_ipv6_address" "transit" {
  interface = fastiron_interface_ve.transit.name
  address   = "2001:db8:3053::1/64"
}

data "fastiron_interface_addresses" "transit" {
  interface = fastiron_interface_ve.transit.name
  depends_on = [
    fastiron_interface_ipv4_address.transit,
    fastiron_interface_ipv6_address.transit,
  ]
}
```

Use canonical CIDR notation with the interface's host address, not the network address. Changing either the IP or prefix length replaces the address relationship and can interrupt connectivity through that address. A different prefix already configured on the same IP must be imported and replaced explicitly.

Import examples:

```sh
tofu import fastiron_interface_ipv4_address.transit 've 3053|ipv4|192.0.2.1/30'
tofu import fastiron_interface_ipv6_address.transit 've 3053|ipv6|2001:db8:3053::1/64'
```

The data source returns both families in its `addresses` set. A VE cannot be destroyed while it still has addresses or other child configuration. Address deletion also rejects remaining VRRP child configuration.

For the physical management interface, use its canonical name:

```hcl
resource "fastiron_interface_ipv4_address" "management" {
  interface = "management 1"
  address   = "198.51.100.10/24"
}
```

Import with `tofu import fastiron_interface_ipv4_address.management 'management 1|ipv4|198.51.100.10/24'`. IPv6 uses the corresponding IPv6 resource. Other management addresses remain independently configured. Preserve a reachable management address when replacing or removing the address used by the provider connection.

The tested firmware accepted management IPv4 `/24` and `/25` and IPv6 `/64`; it rejected a management IPv4 `/32` request. Ethernet and other interface kinds are not yet supported by address resources.
