package stormcontrol

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type interfaceDataSource struct{ device *fastiron.Device }

func NewDataSource() *interfaceDataSource { return &interfaceDataSource{} }

var _ datasource.DataSourceWithConfigure = (*interfaceDataSource)(nil)

func (d *interfaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_storm_control"
}

func (d *interfaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads native storm-control rates on an Ethernet or LAG interface without taking ownership or saving configuration.", Attributes: map[string]schema.Attribute{
		"interface":             schema.StringAttribute{Required: true, Description: "ethernet <stack>/<slot>/<port> or lag <id>."},
		"unit":                  schema.StringAttribute{Computed: true, Description: "Shared rate unit, or null when no limits are configured."},
		"broadcast_limit":       schema.Int64Attribute{Computed: true, Description: "Native broadcast rate limit, or null when disabled."},
		"multicast_limit":       schema.Int64Attribute{Computed: true, Description: "Native multicast rate limit, or null when disabled."},
		"unknown_unicast_limit": schema.Int64Attribute{Computed: true, Description: "Native unknown-unicast rate limit, or null when disabled."},
		"has_native_options":    schema.BoolAttribute{Computed: true, Description: "Whether logging, threshold or shutdown options exist. RESTCONF rate management cannot preserve these options."},
	}}
}

func (d *interfaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *interfaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var query struct {
		Interface      types.String `tfsdk:"interface"`
		Unit           types.String `tfsdk:"unit"`
		Broadcast      types.Int64  `tfsdk:"broadcast_limit"`
		Multicast      types.Int64  `tfsdk:"multicast_limit"`
		UnknownUnicast types.Int64  `tfsdk:"unknown_unicast_limit"`
		Options        types.Bool   `tfsdk:"has_native_options"`
	}
	resp.Diagnostics.Append(req.Config.Get(ctx, &query)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := read(ctx, d.device, query.Interface.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read interface storm control", err.Error())
		return
	}
	state := resourceState(query.Interface.ValueString(), observed.policy, false)
	query.Unit, query.Broadcast, query.Multicast, query.UnknownUnicast = state.Unit, state.Broadcast, state.Multicast, state.UnknownUnicast
	query.Options = types.BoolValue(observed.options)
	resp.Diagnostics.Append(resp.State.Set(ctx, query)...)
}
