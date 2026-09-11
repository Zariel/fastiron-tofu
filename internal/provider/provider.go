package provider

import (
	"context"

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
		func() resource.Resource { return &saveResource{} },
		func() resource.Resource { return &vlanResource{} },
		func() resource.Resource { return &ethernetResource{} },
		func() resource.Resource { return &membershipResource{} },
		func() resource.Resource { return &dnsResource{} },
		func() resource.Resource { return &lldpResource{} },
		func() resource.Resource { return &lldpResource{perInterface: true} },
		func() resource.Resource { return &poeResource{} },
		func() resource.Resource { return &veResource{} },
		func() resource.Resource { return &addressResource{} },
		func() resource.Resource { return &addressResource{ipv6: true} },
		func() resource.Resource { return &lagResource{} },
		func() resource.Resource { return &routeResource{} },
		func() resource.Resource { return &ospfAreaResource{} },
		func() resource.Resource { return &ospfInterfaceResource{} },
		func() resource.Resource { return &stpVLANResource{} },
		func() resource.Resource { return &stpInterfaceResource{} },
	}
}

func (p *fastironProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		func() datasource.DataSource { return &capabilitiesDataSource{} },
		func() datasource.DataSource { return &dnsDataSource{} },
		func() datasource.DataSource { return &lldpDataSource{} },
		func() datasource.DataSource { return &poeDataSource{} },
		func() datasource.DataSource { return &addressesDataSource{} },
		func() datasource.DataSource { return &lagDataSource{} },
		func() datasource.DataSource { return &routesDataSource{} },
		func() datasource.DataSource { return &ospfDataSource{} },
		func() datasource.DataSource { return &stpDataSource{} },
	}
}
