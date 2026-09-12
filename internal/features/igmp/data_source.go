package igmp

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	vlanDataSource struct{ device *fastiron.Device }
	vlanQuery      struct {
		VLANID  types.Int64  `tfsdk:"vlan_id"`
		Mode    types.String `tfsdk:"querier_mode"`
		Version types.Int64  `tfsdk:"version"`
	}
)

func NewVLANDataSource() *vlanDataSource { return &vlanDataSource{} }

var _ datasource.DataSourceWithConfigure = (*vlanDataSource)(nil)

func (d *vlanDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vlan_igmp_snooping"
}

func (d *vlanDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads native IGMP snooping overrides on an existing VLAN without taking ownership or saving configuration. Null fields inherit global settings.", Attributes: map[string]schema.Attribute{
		"vlan_id":      schema.Int64Attribute{Required: true, Description: "Existing VLAN identifier, 1 through 4095."},
		"querier_mode": schema.StringAttribute{Computed: true, Description: "Configured active, passive or disabled override. Null means the global mode is inherited."},
		"version":      schema.Int64Attribute{Computed: true, Description: "Configured version 2 or 3 override. Null means the global version is inherited."},
	}}
}

func (d *vlanDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *vlanDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var query vlanQuery
	resp.Diagnostics.Append(req.Config.Get(ctx, &query)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := read(ctx, d.device, query.VLANID.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read VLAN IGMP overrides", err.Error())
		return
	}
	query.Mode = types.StringNull()
	query.Version = types.Int64Null()
	if observed.Mode != "" {
		query.Mode = types.StringValue(observed.Mode)
	}
	if observed.Version != 0 {
		query.Version = types.Int64Value(observed.Version)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, query)...)
}
