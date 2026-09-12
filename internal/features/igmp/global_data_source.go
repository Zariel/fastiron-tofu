package igmp

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	globalDataSource struct{ device *fastiron.Device }
	globalQuery      struct {
		Mode    types.String `tfsdk:"querier_mode"`
		Version types.Int64  `tfsdk:"version"`
	}
)

func NewGlobalDataSource() *globalDataSource { return &globalDataSource{} }

var _ datasource.DataSourceWithConfigure = (*globalDataSource)(nil)

func (d *globalDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_igmp_snooping"
}

func (d *globalDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads native global IGMP snooping configuration without taking ownership or saving configuration. VLAN and port overrides are outside this query.", Attributes: map[string]schema.Attribute{
		"querier_mode": schema.StringAttribute{Computed: true, Description: "Configured active or passive mode, or disabled when global enablement is absent. VLAN overrides remain independent."},
		"version":      schema.Int64Attribute{Computed: true, Description: "Configured global IGMP version, 2 or 3. Reports 2 when no explicit version is configured."},
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
	observed, err := readGlobal(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read global IGMP policy", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, globalQuery{
		Mode:    types.StringValue(observed.Mode),
		Version: types.Int64Value(observed.Version),
	})...)
}
