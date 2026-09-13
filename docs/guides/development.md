# Build and use a local provider

The provider has not been published. Use a local filesystem mirror for development:

```sh
nix develop
export FASTIRON_MIRROR="$PWD/.provider-mirror"
platform="$(go env GOOS)_$(go env GOARCH)"
provider_dir="$FASTIRON_MIRROR/registry.opentofu.org/zariel/fastiron/0.0.1/$platform"
mkdir -p "$provider_dir"
go build -ldflags '-X main.version=0.0.1' \
  -o "$provider_dir/terraform-provider-fastiron_v0.0.1" \
  ./cmd/terraform-provider-fastiron
cat > .tofurc <<EOF
provider_installation {
  filesystem_mirror {
    path = "$FASTIRON_MIRROR"
  }
}
EOF
export TF_CLI_CONFIG_FILE="$PWD/.tofurc"
tofu -chdir=examples/basic init
```

Provide the example's host, trusted CA, known_hosts contents, and disconnected test port through OpenTofu variables. Supply username/password through `FASTIRON_USERNAME` and `FASTIRON_PASSWORD`. SSH keys and privilege elevation use `FASTIRON_SSH_PRIVATE_KEY` and `FASTIRON_ENABLE_PASSWORD` when needed. `FASTIRON_KNOWN_HOSTS` can supply trusted host-key contents when the provider block omits them.

The provider currently requires SSH even for RESTCONF-backed resources to identify active firmware and verify configuration persistence. Firmware is reported as evidence, not checked against an expected version or allowlist. Omitted VLAN/port names plan a reset, and omitted Ethernet `enabled` plans `true`. Review imports with matching HCL before applying.

See [supported features and tested compatibility](../compatibility.md).

## Test configuration parsing offline

Run `go test -race ./internal/config` inside the development shell. The LLDP tests automatically discover captured configurations in `internal/config/testdata/lldp`: each `.conf` file has a matching `.json` file containing the port inventory and expected global, receive, and transmit settings. Expected settings come from the capture scenario, independently of the parser. Adding a pair adds a test case without changing Go code.

The corpus contains sanitized full configurations captured on FastIron 09.0.10kT213, covering defaults, disabled ports, receive-only and transmit-only modes, ranges, multiple slots, and range regrouping. Credentials are removed and management addresses are anonymized while command structure is preserved. Companion `.rest.json` files, where available, preserve RESTCONF configuration responses, including cached flags that disagree with native configuration; the parser tests do not use those responses as their oracle.

`internal/config/testdata/lldp-med` uses the same paired-file convention for MED network policies. Its expected results identify each port's application policies, including tagging mode, VLAN, priority, and DSCP. Captures cover all eight application types, policy regrouping, single-port deletion, and zero-priority policies emitted as untagged. These are parser fixtures; MED provider resources are not yet implemented.

For additional syntax coverage, capture a planned batch of configurations and expected results on a throwaway switch, then iterate against the saved files offline. Remove credentials and identifying addresses before committing captures. Keep malformed-input and ownership tests alongside the corpus to cover cases a switch would not emit.
