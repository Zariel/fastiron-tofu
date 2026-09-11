# AAA servers

The `fastiron_aaa_radius_server` and `fastiron_aaa_tacacs_server` resources each own one server. Enable AAA writes explicitly:

```hcl
provider "fastiron" {
  host              = "switch.example.net"
  allow_aaa_changes = true
  # Configure transport credentials and trust as described in the provider guide.
}

variable "aaa_secret" {
  type      = string
  sensitive = true
}

resource "fastiron_aaa_radius_server" "authentication" {
  address = "192.0.2.53"
  purpose = "authentication-only"
  secret  = var.aaa_secret
}

resource "fastiron_aaa_tacacs_server" "administrators" {
  address = "192.0.2.54"
  secret  = var.aaa_secret
}
```

| Attribute | RADIUS | TACACS |
|---|---|---|
| `address` | Required; changing it replaces the server | Same |
| `auth_port` | UDP port, default 1812 | TCP port, default 49 |
| `acct_port` | UDP port, default 1813 | Computed null |
| `purpose` | `default`, `authentication-only`, `accounting-only` | Also supports `authorization-only` |
| `secret` | Optional; 1–64 bytes when supplied | Required; 1–32 bytes |
| `persistence_pending` | Reports an operation needing reconciliation or persistence | Same |

Addresses accept canonical unicast IP addresses or lowercase DNS hostnames. Endpoint support depends on firmware; hardware lifecycle testing used IPv4. Keys cannot contain whitespace or control characters.

Shared retry settings, authentication policy, and neighboring servers remain independently managed. Existing native settings outside this resource's RESTCONF ownership, such as RADIUS TLS profiles or per-server dot1x options, cause mutation to fail before changing the server.

## Secrets and replacement

`secret` is sensitive configuration and is stored in OpenTofu state. Protect state and plan files accordingly. The provider never exposes returned server keys or compares their opaque encodings with plaintext. It privately tracks the configured value successfully applied; external changes to the remote secret cannot currently be detected during refresh. Import does not retrieve a usable secret: supply the intended key before managing a keyed server.

Key rotation updates the existing server. Removing a RADIUS `secret` from configuration explicitly replaces that server, because RESTCONF key deletion did not reliably restore native key absence on the tested firmware. Replacement changes the server's position in the authentication attempt order. Keyless TACACS configuration is not currently supported; omitted-key POST/PATCH requests produced a native key clause during hardware testing.

A failed operation can leave partial running configuration. The resource retains reconciliation state where it can observe the server; repeat apply or destroy after resolving the error. `persistence_pending` covers both configuration and save failures. Automatic saves and manual saves follow the provider's `persistence_mode`.

See the [complete AAA example](../../examples/aaa/main.tf) for transport trust and separate RADIUS/TACACS secrets.

## Import

```sh
tofu import fastiron_aaa_radius_server.authentication 'radius-server host 192.0.2.53'
tofu import fastiron_aaa_tacacs_server.administrators 'tacacs-server host 192.0.2.54'
```

## Discovery

```hcl
data "fastiron_aaa_servers" "switch" {}

output "aaa_servers" {
  value = data.fastiron_aaa_servers.switch.servers
}
```

`servers` is a map keyed by `radius|address` or `tacacs|address`. Entries contain `kind`, `address`, `auth_port`, `acct_port` (null for TACACS), and `purpose`. An empty configuration produces an empty map. Discovery does not require AAA write opt-in, change configuration, or verify server reachability and successful authentication. It excludes secrets and does not infer key presence from returned API values.

If discovery must reflect servers changed in the same apply, add `depends_on` referencing those resources to the data source.

AAA user and authentication-policy resources are not yet available. Keyed server CRUD and saved configuration have been tested; end-to-end authentication against a live RADIUS or TACACS service has not.
