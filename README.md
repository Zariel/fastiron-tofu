# fastiron-tofu

An OpenTofu provider for Brocade/RUCKUS FastIron switches, implemented in Go using the Terraform Plugin Framework.

Provider address: `registry.opentofu.org/zariel/fastiron`.
Plugin binary: `terraform-provider-fastiron`.

The provider supports FastIron across ICX models without firmware or model allowlists. Feature availability depends on the switch's actual capabilities. Testing starts with FastIron 09.0.10 on an ICX 7150, with ICX 7250 testing to follow. Versions are documented as tested, not required from users.

The current implementation includes VLAN names/existence, Ethernet port names/enable state, configuration saves, and firmware discovery. Full provider scope is still in progress.

## Development

Enter the pinned tool environment with `nix develop`. It includes Go, gofumpt, gopls, OpenTofu, Make, Jujutsu, Git, curl, jq, Python, OpenSSH, picocom, socat, and nixfmt.

```sh
nix develop
make check
make build
```

Use `gofumpt -w .` for Go and `nix fmt` for the flake. Run repeatable console command batches with `python3 tools/serial_console.py --device /dev/ttyUSB0 'show version'`. The script handles login, privilege elevation, prompt framing, timeouts, and redaction. Credentials come from the same `FASTIRON_*` environment variables as the provider. Serial device permissions are managed by the host operating system.

See [local installation and testing](docs/guides/development.md) and the [basic example](examples/basic/main.tf).
