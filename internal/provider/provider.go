package provider

import (
	"context"

	"github.com/zariel/fastiron-tofu/internal/features/authentication"
	"github.com/zariel/fastiron-tofu/internal/features/dns"
	"github.com/zariel/fastiron-tofu/internal/features/lldp"
	"github.com/zariel/fastiron-tofu/internal/features/ospf"
	"github.com/zariel/fastiron-tofu/internal/features/poe"
	"github.com/zariel/fastiron-tofu/internal/features/route"
	"github.com/zariel/fastiron-tofu/internal/features/stp"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

type fastironProvider struct{ version string }

func New(version string) *fastironProvider { return &fastironProvider{version: version} }

func (p *fastironProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "fastiron"
	resp.Version = p.version
}

func (p *fastironProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerSchema()
}

func (p *fastironProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource { return &userResource{} },
		func() resource.Resource { return &aaaResource{} },
		func() resource.Resource { return &aaaServerResource{kind: "radius"} },
		func() resource.Resource { return &aaaServerResource{kind: "tacacs"} },
		func() resource.Resource { return &saveResource{} },
		func() resource.Resource { return &vlanResource{} },
		func() resource.Resource { return &ethernetResource{} },
		func() resource.Resource { return &membershipResource{} },
		func() resource.Resource { return &dns.Resource{} },
		func() resource.Resource { return lldp.NewGlobalResource() },
		func() resource.Resource { return lldp.NewInterfaceResource() },
		func() resource.Resource { return &poe.Resource{} },
		func() resource.Resource { return &veResource{} },
		func() resource.Resource { return &addressResource{} },
		func() resource.Resource { return &addressResource{ipv6: true} },
		func() resource.Resource { return &lagResource{} },
		func() resource.Resource { return &route.Resource{} },
		func() resource.Resource { return &ospf.AreaResource{} },
		func() resource.Resource { return &ospf.InterfaceResource{} },
		func() resource.Resource { return &stp.VLANResource{} },
		func() resource.Resource { return &stp.InterfaceResource{} },
		func() resource.Resource { return &authentication.InterfaceResource{} },
	}
}

func (p *fastironProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		func() datasource.DataSource { return &capabilitiesDataSource{} },
		func() datasource.DataSource { return &dns.DataSource{} },
		func() datasource.DataSource { return &lldp.DataSource{} },
		func() datasource.DataSource { return &poe.DataSource{} },
		func() datasource.DataSource { return &addressesDataSource{} },
		func() datasource.DataSource { return &lagDataSource{} },
		func() datasource.DataSource { return &route.DataSource{} },
		func() datasource.DataSource { return &ospf.DataSource{} },
		func() datasource.DataSource { return &stp.DataSource{} },
		func() datasource.DataSource { return &aaaServersDataSource{} },
		func() datasource.DataSource { return &usersDataSource{} },
		func() datasource.DataSource { return &aaaDataSource{} },
		func() datasource.DataSource { return &authentication.InterfacesDataSource{} },
	}
}
