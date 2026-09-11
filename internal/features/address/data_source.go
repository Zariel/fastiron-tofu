package address

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	DataSource              struct{ device *fastiron.Device }
	interfaceAddressesModel struct {
		Interface types.String `tfsdk:"interface"`
		Addresses types.Set    `tfsdk:"addresses"`
	}
)

func (d *DataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_addresses"
}

func (d *DataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured IPv4 and IPv6 addresses on a VE or management interface without taking ownership.", Attributes: map[string]schema.Attribute{
		"interface": schema.StringAttribute{Required: true, Description: "Canonical VE or management interface name."},
		"addresses": schema.SetAttribute{Computed: true, ElementType: types.StringType, Description: "Configured host addresses and prefix lengths in CIDR syntax."},
	}}
}

func (d *DataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *DataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m interfaceAddressesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	values := []string{}
	for _, ipv6 := range []bool{false, true} {
		prefixes, err := readAddresses(ctx, d.device, m.Interface.ValueString(), ipv6)
		if err != nil {
			resp.Diagnostics.AddError("Cannot read interface addresses", err.Error())
			return
		}
		for _, prefix := range prefixes {
			values = append(values, prefix.String())
		}
	}
	addresses, diags := types.SetValueFrom(ctx, types.StringType, values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.Addresses = addresses
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
