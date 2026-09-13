package ethernet

import (
	"context"
	"math/big"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type dataSource struct {
	device     *fastiron.Device
	collection bool
}

type queryModel struct {
	Port        types.String            `tfsdk:"port"`
	Name        types.String            `tfsdk:"name"`
	PortName    types.String            `tfsdk:"port_name"`
	Enabled     types.Bool              `tfsdk:"enabled"`
	IfIndex     types.Int64             `tfsdk:"ifindex"`
	AdminStatus types.String            `tfsdk:"admin_status"`
	OperStatus  types.String            `tfsdk:"oper_status"`
	Counters    map[string]types.Number `tfsdk:"counters"`
	Link        *linkModel              `tfsdk:"link"`
}

type linkModel struct {
	AutoNegotiate         types.Bool   `tfsdk:"auto_negotiate"`
	Duplex                types.String `tfsdk:"duplex"`
	Speed                 types.String `tfsdk:"speed"`
	Clock                 types.String `tfsdk:"clock"`
	ReportedAutoNegotiate types.Bool   `tfsdk:"reported_auto_negotiate"`
	ReportedDuplex        types.String `tfsdk:"reported_duplex"`
	NegotiatedDuplex      types.String `tfsdk:"negotiated_duplex"`
	NegotiatedSpeed       types.String `tfsdk:"negotiated_speed"`
	NegotiatedClock       types.String `tfsdk:"negotiated_clock"`
}

func NewDataSource() *dataSource          { return &dataSource{} }
func NewInventoryDataSource() *dataSource { return &dataSource{collection: true} }

var _ datasource.DataSourceWithConfigure = (*dataSource)(nil)

func (d *dataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_ethernet"
	if d.collection {
		resp.TypeName = req.ProviderTypeName + "_ethernet_interfaces"
	}
}

func (d *dataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	link := map[string]schema.Attribute{
		"auto_negotiate":          schema.BoolAttribute{Computed: true, Description: "Autonegotiation reported by the RESTCONF configuration projection, or null if omitted."},
		"duplex":                  schema.StringAttribute{Computed: true, Description: "Duplex mode reported by the RESTCONF configuration projection, or null if omitted."},
		"speed":                   schema.StringAttribute{Computed: true, Description: "Speed identity reported by the RESTCONF configuration projection, or null if omitted."},
		"clock":                   schema.StringAttribute{Computed: true, Description: "Ethernet clock mode reported by the RESTCONF configuration projection, or null if omitted."},
		"reported_auto_negotiate": schema.BoolAttribute{Computed: true, Description: "Autonegotiation reported in interface state, or null if omitted."},
		"reported_duplex":         schema.StringAttribute{Computed: true, Description: "Duplex mode reported in interface state, or null if omitted."},
		"negotiated_duplex":       schema.StringAttribute{Computed: true, Description: "Reported negotiated duplex, or null if omitted."},
		"negotiated_speed":        schema.StringAttribute{Computed: true, Description: "Reported negotiated speed identity, including SPEED_UNKNOWN when reported by the switch."},
		"negotiated_clock":        schema.StringAttribute{Computed: true, Description: "Reported negotiated clock mode, or null if omitted."},
	}
	attributes := map[string]schema.Attribute{
		"port":         schema.StringAttribute{Required: !d.collection, Computed: d.collection, Description: "Canonical stack/slot/port identity."},
		"name":         schema.StringAttribute{Computed: true, Description: "Canonical Ethernet interface name."},
		"port_name":    schema.StringAttribute{Computed: true, Description: "Configured port description."},
		"enabled":      schema.BoolAttribute{Computed: true, Description: "Configured administrative enable state."},
		"ifindex":      schema.Int64Attribute{Computed: true, Description: "Reported interface index, or null if omitted."},
		"admin_status": schema.StringAttribute{Computed: true, Description: "Reported administrative status, or null if omitted."},
		"oper_status":  schema.StringAttribute{Computed: true, Description: "Reported operational status, or null if omitted."},
		"counters":     schema.MapAttribute{Computed: true, ElementType: types.NumberType, Description: "Reported counters keyed by their native names, such as in-octets and out-errors. Values preserve unsigned 64-bit precision. Null means counters were omitted."},
		"link":         schema.SingleNestedAttribute{Computed: true, Attributes: link, Description: "RESTCONF link configuration and operational observations. Configuration metadata can remain stale after unsupported speed resets; it does not prove native automatic speed. Null fields indicate omitted values."},
	}
	description := "Reads one physical Ethernet interface without changing or saving configuration. Operational values are observations and are not managed by the Ethernet resource."
	if d.collection {
		attributes = map[string]schema.Attribute{"interfaces": schema.MapNestedAttribute{Computed: true, Description: "Physical Ethernet interfaces keyed by canonical interface name. Logical interfaces are excluded.", NestedObject: schema.NestedAttributeObject{Attributes: attributes}}}
		description = "Reads physical Ethernet interface configuration, status, link observations and counters without changing or saving configuration."
	}
	resp.Schema = schema.Schema{Description: description, Attributes: attributes}
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
	var port types.String
	if !d.collection {
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("port"), &port)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if err := validate(config{Port: port.ValueString()}); err != nil {
			resp.Diagnostics.AddError("Invalid Ethernet identity", err.Error())
			return
		}
	}
	observed, err := readObservations(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read Ethernet interfaces", err.Error())
		return
	}
	if d.collection {
		interfaces := make(map[string]queryModel, len(observed))
		for _, current := range observed {
			interfaces["ethernet "+current.Port] = queryState(current)
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, struct {
			Interfaces map[string]queryModel `tfsdk:"interfaces"`
		}{Interfaces: interfaces})...)
		return
	}
	for _, current := range observed {
		if current.Port == port.ValueString() {
			resp.Diagnostics.Append(resp.State.Set(ctx, queryState(current))...)
			return
		}
	}
	resp.Diagnostics.AddError("Ethernet interface not found", "The RESTCONF interface inventory does not contain ethernet "+port.ValueString()+".")
}

func queryState(observed observation) queryModel {
	value := queryModel{
		Port: types.StringValue(observed.Port), Name: types.StringValue("ethernet " + observed.Port),
		PortName: types.StringValue(observed.PortName), Enabled: types.BoolValue(observed.Enabled),
		IfIndex: types.Int64Null(), AdminStatus: types.StringPointerValue(observed.AdminStatus), OperStatus: types.StringPointerValue(observed.OperStatus),
	}
	if observed.IfIndex != nil {
		value.IfIndex = types.Int64Value(int64(*observed.IfIndex))
	}
	if observed.Counters != nil {
		value.Counters = make(map[string]types.Number, len(observed.Counters))
	}
	for name, counter := range observed.Counters {
		// Start with an uninitialized Float so SetUint64 selects enough precision.
		value.Counters[name] = types.NumberValue(new(big.Float).SetUint64(counter))
	}
	if link := observed.Link; link != nil {
		value.Link = &linkModel{
			AutoNegotiate: types.BoolPointerValue(link.AutoNegotiate), Duplex: types.StringPointerValue(link.Duplex), Speed: types.StringPointerValue(link.Speed), Clock: types.StringPointerValue(link.Clock),
			ReportedAutoNegotiate: types.BoolPointerValue(link.ReportedAutoNegotiate), ReportedDuplex: types.StringPointerValue(link.ReportedDuplex),
			NegotiatedDuplex: types.StringPointerValue(link.NegotiatedDuplex), NegotiatedSpeed: types.StringPointerValue(link.NegotiatedSpeed), NegotiatedClock: types.StringPointerValue(link.NegotiatedClock),
		}
	}
	return value
}
