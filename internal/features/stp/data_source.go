package stp

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	DataSource struct{ device *fastiron.Device }
	stpModel   struct {
		VLANs      map[string]stpVLANStatusModel      `tfsdk:"vlans"`
		Interfaces map[string]stpInterfaceStatusModel `tfsdk:"interfaces"`
	}
	stpInterfaceStatusModel struct {
		AdminEdge types.Bool `tfsdk:"admin_edge"`
		BPDUGuard types.Bool `tfsdk:"bpdu_guard"`
		RootGuard types.Bool `tfsdk:"root_guard"`
	}
	stpVLANStatusModel struct {
		Mode     types.String `tfsdk:"mode"`
		Priority types.Int64  `tfsdk:"priority"`
	}
)

func (d *DataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_spanning_tree"
}

func (d *DataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured VLAN and interface spanning-tree settings.", Attributes: map[string]schema.Attribute{
		"interfaces": schema.MapNestedAttribute{Computed: true, Description: "Native interface flags keyed by interface name, including CLI-only settings. Cached interface identities without native flags report false defaults. Interfaces with no explicit settings may be omitted.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"admin_edge": schema.BoolAttribute{Computed: true, Description: "Configured RSTP edge-port setting."},
			"bpdu_guard": schema.BoolAttribute{Computed: true, Description: "Configured BPDU guard setting."},
			"root_guard": schema.BoolAttribute{Computed: true, Description: "Configured root protection setting."},
		}}},
		"vlans": schema.MapNestedAttribute{Computed: true, Description: "Enabled spanning-tree configurations keyed by VLAN ID.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"mode":     schema.StringAttribute{Computed: true, Description: "stp or rstp."},
			"priority": schema.Int64Attribute{Computed: true, Description: "Configured bridge priority."},
		}}},
	}}
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
	vlans, err := readVLANs(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read spanning-tree configuration", err.Error())
		return
	}
	interfaces, err := readInterfaces(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read spanning-tree interfaces", err.Error())
		return
	}

	m := stpModel{VLANs: map[string]stpVLANStatusModel{}, Interfaces: map[string]stpInterfaceStatusModel{}}
	for _, vlan := range vlans {
		m.VLANs[strconv.FormatInt(vlan.VLANID, 10)] = stpVLANStatusModel{Mode: types.StringValue(vlan.Mode), Priority: types.Int64Value(vlan.Priority)}
	}
	for name, settings := range interfaces {
		m.Interfaces[name] = stpInterfaceStatusModel{AdminEdge: types.BoolValue(settings.AdminEdge), BPDUGuard: types.BoolValue(settings.BPDUGuard), RootGuard: types.BoolValue(settings.RootGuard)}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
