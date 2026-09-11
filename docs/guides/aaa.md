# AAA server discovery

Read configured RADIUS and TACACS servers through RESTCONF:

```hcl
data "fastiron_aaa_servers" "switch" {}

output "aaa_servers" {
  value = data.fastiron_aaa_servers.switch.servers
}
```

`servers` is a map keyed by `radius|address` or `tacacs|address`. Each entry reports:

| Attribute | Meaning |
|---|---|
| `kind` | `radius` or `tacacs` |
| `address` | Configured server address |
| `auth_port` | RADIUS authentication UDP port or TACACS TCP port |
| `acct_port` | RADIUS accounting UDP port; null for TACACS |
| `purpose` | `default`, `authentication-only`, `accounting-only`, or TACACS `authorization-only` |

An empty configuration produces an empty map. Discovery does not change AAA configuration or verify server reachability, credentials, or successful authentication. Secret material is excluded from the data source and its state; the API's returned secret value does not reliably indicate whether a per-server key was configured.

AAA server, user, and authentication-policy configuration resources are not yet available.
