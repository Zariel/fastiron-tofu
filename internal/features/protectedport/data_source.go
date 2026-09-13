package protectedport

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
	resp.TypeName = req.ProviderTypeName + "_interface_protected_port"
}

func (d *interfaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads native protected-port configuration on an Ethernet or LAG interface without taking ownership or saving configuration.", Attributes: map[string]schema.Attribute{
		"interface": schema.StringAttribute{Required: true, Description: "ethernet <stack>/<slot>/<port> or lag <id>."},
		"enabled":   schema.BoolAttribute{Computed: true, Description: "Whether native protected-port configuration is enabled. This is configuration, not a packet isolation measurement."},
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
		Interface types.String `tfsdk:"interface"`
		Enabled   types.Bool   `tfsdk:"enabled"`
	}
	resp.Diagnostics.Append(req.Config.Get(ctx, &query)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := read(ctx, d.device, query.Interface.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read interface protected port", err.Error())
		return
	}
	query.Enabled = types.BoolValue(observed.enabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, query)...)
}
