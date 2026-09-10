# fastiron-tofu

An OpenTofu provider for Brocade/RUCKUS FastIron switches, implemented in Go using the Terraform Plugin Framework.

Provider address: `registry.opentofu.org/zariel/fastiron`.
Plugin binary: `terraform-provider-fastiron`.

Implementation targets FastIron 09.0.10 across ICX models. Feature availability depends on firmware, operating mode, licenses, and hardware capabilities. Initial hardware testing will use an ICX 7150, with ICX 7250 testing to follow; neither is an exclusive model requirement.

## Development

Enter the pinned tool environment with `nix develop`. It includes Go, gopls, OpenTofu, Make, Jujutsu, Git, curl, jq, Python, OpenSSH, picocom, socat, and nixfmt.

```sh
nix develop
make check
make build
```

Use `nix fmt` to format the flake. To access a serial console, run `picocom --baud 9600 /dev/serial/by-id/<adapter>` with the switch's actual console baud rate and device path. Exit picocom with Ctrl-A, Ctrl-X. Serial device permissions are managed by the host operating system.
