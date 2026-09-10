# Read LAG configuration

`fastiron_lags` reports configured link aggregation groups and their Ethernet members through RESTCONF.

```hcl
data "fastiron_lags" "switch" {}

output "lags" {
  value = data.fastiron_lags.switch.lags
}
```

The `lags` map is keyed by canonical interface name, such as `lag 1`. Each entry contains:

- `lag_id`: numeric identity.
- `name`: configured LAG name, such as `uplink`.
- `mode`: `dynamic` for LACP or `static`.
- `members`: a set of canonical Ethernet names, such as `ethernet 1/2/1`.

Membership describes configuration, including disconnected members. It does not indicate link health or whether LACP has formed a working aggregate.
