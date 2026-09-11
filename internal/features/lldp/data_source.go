package lldp

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	DataSource      struct{ device *fastiron.Device }
	interfacesModel struct {
		Interfaces types.Map `tfsdk:"interfaces"`
	}
)

func (d *DataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lldp_interfaces"
}

func (d *DataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads LLDP enable configuration for all available interfaces without taking ownership.", Attributes: map[string]schema.Attribute{"interfaces": schema.MapAttribute{Computed: true, ElementType: types.BoolType, Description: "Canonical interface names mapped to their configured LLDP enable state."}}}
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

func (d *DataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	values, err := readInterfaces(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LLDP interfaces", err.Error())
		return
	}
	interfaces, diags := types.MapValueFrom(ctx, types.BoolType, values)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, interfacesModel{Interfaces: interfaces})...)
}
