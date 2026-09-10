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

