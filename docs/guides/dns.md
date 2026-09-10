# DNS servers

Each `fastiron_ip_dns_server` owns one address and preserves other servers. Changing its address replaces that relationship. Destroy removes only that address.

```hcl
resource "fastiron_ip_dns_server" "resolver" {
  address = "10.1.2.53"
}

data "fastiron_ip_dns_servers" "configured" {
  depends_on = [fastiron_ip_dns_server.resolver]
}
```

Import an existing server with `tofu import fastiron_ip_dns_server.resolver 'ip dns server-address 10.1.2.53'`.

Use a canonical IP address. The tested 09.0.10kT213 build accepted IPv4 and rejected an IPv6 DNS server with an application error. The provider reports endpoint failures without requiring a firmware version or restricting device models.

The collection data source returns the configured `addresses` set without taking ownership of entries.
