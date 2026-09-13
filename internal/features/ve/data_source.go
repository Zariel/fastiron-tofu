package ve

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type dataSource struct{ device *fastiron.Device }

type queryModel struct {
	VEID     types.Int64  `tfsdk:"ve_id"`
	VLANID   types.Int64  `tfsdk:"vlan_id"`
	Name     types.String `tfsdk:"name"`
	PortName types.String `tfsdk:"port_name"`
	Enabled  types.Bool   `tfsdk:"enabled"`
}

func NewDataSource() *dataSource { return &dataSource{} }

var _ datasource.DataSourceWithConfigure = (*dataSource)(nil)

func (d *dataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_ve"
}

func (d *dataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads a VE's native configuration without taking ownership or saving. Requires RESTCONF and SSH access; a missing VE is an error.", Attributes: map[string]schema.Attribute{
		"ve_id":     schema.Int64Attribute{Required: true, Description: "VE identifier, 1 through 4095."},
		"vlan_id":   schema.Int64Attribute{Computed: true, Description: "Corresponding VLAN identifier."},
		"name":      schema.StringAttribute{Computed: true, Description: "Canonical interface name, such as ve 53."},
		"port_name": schema.StringAttribute{Computed: true, Description: "Configured native port name; empty when omitted."},
		"enabled":   schema.BoolAttribute{Computed: true, Description: "Native administrative enable state. This does not report link or routing readiness."},
	}}
}

func (d *dataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *dataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state queryModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.VEID.ValueInt64()
	if err := validate(config{ID: id, VLANID: id}); err != nil {
		resp.Diagnostics.AddError("Invalid VE identity", err.Error())
		return
	}
	if _, err := d.device.Discover(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot discover switch", err.Error())
		return
	}
	observed, err := read(ctx, d.device, id)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read VE", err.Error())
		return
	}

	state.VLANID = types.Int64Value(id)
	state.Name = types.StringValue("ve " + strconv.FormatInt(id, 10))
	state.PortName = types.StringValue(observed.PortName)
	state.Enabled = types.BoolValue(observed.Enabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
