package provider

import (
	"context"

	"github.com/zariel/fastiron-tofu/internal/features/aaa"
	"github.com/zariel/fastiron-tofu/internal/features/acl"
	"github.com/zariel/fastiron-tofu/internal/features/address"
	"github.com/zariel/fastiron-tofu/internal/features/authentication"
	"github.com/zariel/fastiron-tofu/internal/features/dns"
	"github.com/zariel/fastiron-tofu/internal/features/ethernet"
	"github.com/zariel/fastiron-tofu/internal/features/lag"
	"github.com/zariel/fastiron-tofu/internal/features/lldp"
	"github.com/zariel/fastiron-tofu/internal/features/ospf"
	"github.com/zariel/fastiron-tofu/internal/features/poe"
	"github.com/zariel/fastiron-tofu/internal/features/route"
	"github.com/zariel/fastiron-tofu/internal/features/stp"
	"github.com/zariel/fastiron-tofu/internal/features/system"
	"github.com/zariel/fastiron-tofu/internal/features/ve"
	"github.com/zariel/fastiron-tofu/internal/features/vlan"

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
		func() resource.Resource { return &aaa.UserResource{} },
		func() resource.Resource { return &aaa.PolicyResource{} },
		func() resource.Resource { return aaa.NewRADIUSResource() },
		func() resource.Resource { return aaa.NewTACACSResource() },
		func() resource.Resource { return &system.SaveResource{} },
		func() resource.Resource { return &vlan.Resource{} },
		func() resource.Resource { return &ethernet.Resource{} },
		func() resource.Resource { return &vlan.MembershipResource{} },
		func() resource.Resource { return &dns.Resource{} },
		func() resource.Resource { return lldp.NewGlobalResource() },
		func() resource.Resource { return lldp.NewInterfaceResource() },
		func() resource.Resource { return &poe.Resource{} },
		func() resource.Resource { return &ve.Resource{} },
		func() resource.Resource { return address.NewIPv4Resource() },
		func() resource.Resource { return address.NewIPv6Resource() },
		func() resource.Resource { return &lag.Resource{} },
		func() resource.Resource { return &route.Resource{} },
		func() resource.Resource { return &ospf.AreaResource{} },
		func() resource.Resource { return &ospf.InterfaceResource{} },
		func() resource.Resource { return &stp.VLANResource{} },
		func() resource.Resource { return &stp.InterfaceResource{} },
		func() resource.Resource { return &authentication.InterfaceResource{} },
		func() resource.Resource { return &authentication.GlobalResource{} },
		func() resource.Resource { return &acl.StandardResource{} },
	}
}

func (p *fastironProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		func() datasource.DataSource { return &system.CapabilitiesDataSource{} },
		func() datasource.DataSource { return &dns.DataSource{} },
		func() datasource.DataSource { return &lldp.DataSource{} },
		func() datasource.DataSource { return &poe.DataSource{} },
		func() datasource.DataSource { return &address.DataSource{} },
		func() datasource.DataSource { return &lag.DataSource{} },
		func() datasource.DataSource { return &route.DataSource{} },
		func() datasource.DataSource { return &ospf.DataSource{} },
		func() datasource.DataSource { return &stp.DataSource{} },
		func() datasource.DataSource { return &aaa.ServersDataSource{} },
		func() datasource.DataSource { return &aaa.UsersDataSource{} },
		func() datasource.DataSource { return &aaa.PolicyDataSource{} },
		func() datasource.DataSource { return &authentication.InterfacesDataSource{} },
		func() datasource.DataSource { return &authentication.GlobalDataSource{} },
	}
}
