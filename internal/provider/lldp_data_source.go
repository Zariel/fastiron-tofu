package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	lldpDataSource      struct{ device *fastiron.Device }
	lldpInterfacesModel struct {
		Interfaces types.Map `tfsdk:"interfaces"`
	}
)

func (d *lldpDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lldp_interfaces"
}

func (d *lldpDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads LLDP enable configuration for all available interfaces without taking ownership.", Attributes: map[string]schema.Attribute{"interfaces": schema.MapAttribute{Computed: true, ElementType: types.BoolType, Description: "Canonical interface names mapped to their configured LLDP enable state."}}}
}

func (d *lldpDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *lldpDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	values, err := d.device.LLDPInterfaces(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LLDP interfaces", err.Error())
		return
	}
	interfaces, diags := types.MapValueFrom(ctx, types.BoolType, values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, lldpInterfacesModel{Interfaces: interfaces})...)
}
