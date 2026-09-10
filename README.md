# fastiron-tofu

An OpenTofu provider for Brocade/RUCKUS FastIron switches, implemented in Go using the Terraform Plugin Framework.

Provider address: `registry.opentofu.org/zariel/fastiron`.
Plugin binary: `terraform-provider-fastiron`.

Implementation targets FastIron 09.0.10 across ICX models. Feature availability depends on firmware, operating mode, licenses, and hardware capabilities. Initial hardware testing will use an ICX 7150, with ICX 7250 testing to follow; neither is an exclusive model requirement.

