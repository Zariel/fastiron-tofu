package lag

import (
	"context"
	"math/big"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type interfaceDataSource struct{ device *fastiron.Device }

type interfaceQueryModel struct {
	LAGID       types.Int64             `tfsdk:"lag_id"`
	Name        types.String            `tfsdk:"name"`
	PortName    types.String            `tfsdk:"port_name"`
	Enabled     types.Bool              `tfsdk:"enabled"`
	IfIndex     types.Int64             `tfsdk:"ifindex"`
	AdminStatus types.String            `tfsdk:"admin_status"`
	OperStatus  types.String            `tfsdk:"oper_status"`
	Counters    map[string]types.Number `tfsdk:"counters"`
}

func NewInterfaceDataSource() *interfaceDataSource { return &interfaceDataSource{} }

var _ datasource.DataSourceWithConfigure = (*interfaceDataSource)(nil)

func (d *interfaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_lag"
}

func (d *interfaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads native LAG interface configuration and reported status and counters without writing or saving. Requires RESTCONF and SSH access; a missing native LAG is an error.", Attributes: map[string]schema.Attribute{
		"lag_id":       schema.Int64Attribute{Required: true, Description: "Positive LAG identifier."},
		"name":         schema.StringAttribute{Computed: true, Description: "Canonical interface name, such as lag 5."},
		"port_name":    schema.StringAttribute{Computed: true, Description: "Native virtual-interface description, distinct from the aggregate name and individual member names."},
		"enabled":      schema.BoolAttribute{Computed: true, Description: "Native virtual-interface administrative state. Individual members may be disabled even when this is true; it does not indicate link readiness."},
		"ifindex":      schema.Int64Attribute{Computed: true, Description: "Reported interface index, or null if omitted."},
		"admin_status": schema.StringAttribute{Computed: true, Description: "Reported administrative status, or null if omitted."},
		"oper_status":  schema.StringAttribute{Computed: true, Description: "Reported operational status, or null if omitted. This does not report individual LACP partner state."},
		"counters":     schema.MapAttribute{Computed: true, ElementType: types.NumberType, Description: "Reported interface counters with unsigned 64-bit precision. Null means counters were omitted."},
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
	var state interfaceQueryModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.LAGID.ValueInt64()
	if id < 1 {
		resp.Diagnostics.AddError("Invalid LAG identity", "lag_id must be positive.")
		return
	}

	if _, err := d.device.Discover(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot discover switch", err.Error())
		return
	}

	name := "lag " + strconv.FormatInt(id, 10)
	document, err := d.device.L2Config(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LAG interface", err.Error())
		return
	}
	observed, err := document.LAGInterface(id)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LAG interface", err.Error())
		return
	}

	state.Name = types.StringValue(name)
	state.PortName = types.StringValue(observed.PortName)
	state.Enabled = types.BoolValue(observed.Enabled)
	status, err := readStatus(ctx, d.device, name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LAG interface status", err.Error())
		return
	}
	state.IfIndex = types.Int64Null()
	if status.IfIndex != nil {
		state.IfIndex = types.Int64Value(int64(*status.IfIndex))
	}
	state.AdminStatus = types.StringPointerValue(status.AdminStatus)
	state.OperStatus = types.StringPointerValue(status.OperStatus)
	state.Counters = nil
	if status.Counters != nil {
		state.Counters = make(map[string]types.Number, len(status.Counters))
	}
	for name, value := range status.Counters {
		state.Counters[name] = types.NumberValue(new(big.Float).SetUint64(value))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
