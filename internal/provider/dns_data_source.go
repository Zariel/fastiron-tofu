package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	dnsDataSource   struct{ device *fastiron.Device }
	dnsServersModel struct {
		Addresses types.Set `tfsdk:"addresses"`
	}
)

func (d *dnsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_dns_servers"
}

func (d *dnsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads all configured DNS server addresses without taking ownership.", Attributes: map[string]schema.Attribute{"addresses": schema.SetAttribute{Computed: true, ElementType: types.StringType, Description: "Configured DNS server addresses."}}}
}

func (d *dnsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *dnsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	servers, err := d.device.DNSServers(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read DNS servers", err.Error())
		return
	}
	addresses, diags := types.SetValueFrom(ctx, types.StringType, servers)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, dnsServersModel{Addresses: addresses})...)
}
