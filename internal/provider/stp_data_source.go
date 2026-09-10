package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	stpDataSource struct{ device *fastiron.Device }
	stpModel      struct {
		VLANs map[string]stpVLANStatusModel `tfsdk:"vlans"`
	}
	stpVLANStatusModel struct {
		Mode     types.String `tfsdk:"mode"`
		Priority types.Int64  `tfsdk:"priority"`
	}
)

func (d *stpDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_spanning_tree"
}

func (d *stpDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured per-VLAN spanning-tree modes and bridge priorities.", Attributes: map[string]schema.Attribute{
		"vlans": schema.MapNestedAttribute{Computed: true, Description: "Enabled spanning-tree configurations keyed by VLAN ID.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"mode":     schema.StringAttribute{Computed: true, Description: "stp or rstp."},
			"priority": schema.Int64Attribute{Computed: true, Description: "Configured bridge priority."},
		}}},
	}}
}

func (d *stpDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *stpDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	vlans, err := d.device.STPVLANs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read spanning-tree configuration", err.Error())
		return
	}
	m := stpModel{VLANs: map[string]stpVLANStatusModel{}}
	for _, vlan := range vlans {
		m.VLANs[strconv.FormatInt(vlan.VLANID, 10)] = stpVLANStatusModel{Mode: types.StringValue(vlan.Mode), Priority: types.Int64Value(vlan.Priority)}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
