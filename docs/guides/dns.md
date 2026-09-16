# DNS servers

`fastiron_ip_dns_server` owns one configured DNS server address. Other addresses remain independently managed. Changing the address replaces the resource.

```hcl
resource "fastiron_ip_dns_server" "primary" {
  address = "192.0.2.53"
}

data "fastiron_ip_dns_servers" "configured" {
  depends_on = [fastiron_ip_dns_server.primary]
}
```

Reads, imports, drift detection, and write verification use native running configuration. RESTCONF must expose its DNS endpoint, but its cached server list does not establish whether an address is configured. A successful RESTCONF response without the requested native change fails reconciliation and is not saved.

Writes also verify that unrelated native settings remain unchanged, including other servers' order, DHCP-learned entries, domain lists, and interface settings. A failed preservation check reports an error without saving or automatically rolling back. Inspect and repair the unexpected change before retrying; a partially created server remains addressable in state with `persistence_pending = true`.

The query returns an `addresses` set containing configured IPv4 and IPv6 servers. DHCP-learned entries marked as dynamic in native configuration are excluded. On tested FastIron `09.0.10kT213`, RESTCONF omitted a configured IPv6 server; the native query correctly returned it alongside the IPv4 servers, without changing running or saved configuration.

IPv4 server writes have succeeded on the tested firmware. Its RESTCONF endpoint rejected IPv6 server writes, so IPv6 discovery does not imply IPv6 configuration support. Domain search lists and server ordering are not managed by this resource. The query reports configuration, not DNS reachability or successful name resolution.

Import an existing server:

```sh
tofu import fastiron_ip_dns_server.primary 'ip dns server-address 192.0.2.53'
```
