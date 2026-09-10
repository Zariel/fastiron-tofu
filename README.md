# fastiron-tofu

An OpenTofu provider for Brocade/RUCKUS FastIron switches, implemented in Go using the Terraform Plugin Framework.

Provider address: `registry.opentofu.org/zariel/fastiron`.
Plugin binary: `terraform-provider-fastiron`.

The provider supports FastIron across ICX models without firmware or model allowlists. Feature availability depends on the switch's actual capabilities. Testing starts with FastIron 09.0.10 on an ICX 7150, with ICX 7250 testing to follow. Versions are documented as tested, not required from users.

## Development

Enter the pinned tool environment with `nix develop`. It includes Go, gofumpt, gopls, OpenTofu, Make, Jujutsu, Git, curl, jq, Python, OpenSSH, picocom, socat, and nixfmt.

```sh
nix develop
make check
make build
```

Use `gofumpt -w .` to format Go files. Use `nix fmt` to format the flake. To access a serial console, run `picocom --baud 9600 /dev/serial/by-id/<adapter>` with the switch's actual console baud rate and device path. Exit picocom with Ctrl-A, Ctrl-X. Serial device permissions are managed by the host operating system.
