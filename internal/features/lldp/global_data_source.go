package lldp

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type globalDataSource struct{ device *fastiron.Device }

func NewGlobalDataSource() *globalDataSource { return &globalDataSource{} }

func (d *globalDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lldp"
}

func (d *globalDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads global LLDP enable configuration without taking ownership or saving configuration.", Attributes: map[string]schema.Attribute{
		"enabled": schema.BoolAttribute{Computed: true, Description: "Whether LLDP is globally enabled. Per-port operating modes are configured independently."},
	}}
}

func (d *globalDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	device, ok := req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
		return
	}
	d.device = device
}

func (d *globalDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	enabled, err := readEnabled(ctx, d.device, "")
	if err != nil {
		resp.Diagnostics.AddError("Cannot read global LLDP", err.Error())
		return
	}
	state := struct {
		Enabled types.Bool `tfsdk:"enabled"`
	}{Enabled: types.BoolValue(enabled)}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
