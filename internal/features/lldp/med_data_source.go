package lldp

import (
	"context"
	"maps"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type medDataSource struct{ device *fastiron.Device }

func NewMEDDataSource() *medDataSource { return &medDataSource{} }

func (d *medDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lldp_med_policies"
}

func (d *medDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads native LLDP-MED network policies without taking ownership or saving configuration.", Attributes: map[string]schema.Attribute{
		"policies": schema.ListNestedAttribute{Computed: true, Description: "Configured policies sorted by Ethernet interface and application. An empty list means no configured policies.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"interface":   schema.StringAttribute{Computed: true, Description: "Canonical Ethernet interface name."},
			"application": schema.StringAttribute{Computed: true, Description: "MED application type."},
			"traffic":     schema.StringAttribute{Computed: true, Description: "Native tagging mode: untagged, priority-tagged, or tagged."},
			"vlan_id":     schema.Int64Attribute{Computed: true, Description: "Tagged VLAN ID, or null for other modes."},
			"priority":    schema.Int64Attribute{Computed: true, Description: "Layer 2 priority, or null for untagged policies."},
			"dscp":        schema.Int64Attribute{Computed: true, Description: "Layer 3 differentiated services codepoint."},
		}}},
	}}
}

func (d *medDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

type medQueryPolicy struct {
	Interface   types.String `tfsdk:"interface"`
	Application types.String `tfsdk:"application"`
	Traffic     types.String `tfsdk:"traffic"`
	VLAN        types.Int64  `tfsdk:"vlan_id"`
	Priority    types.Int64  `tfsdk:"priority"`
	DSCP        types.Int64  `tfsdk:"dscp"`
}

func (d *medDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	policies, _, err := readMED(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LLDP-MED policies", err.Error())
		return
	}
	state := struct {
		Policies []medQueryPolicy `tfsdk:"policies"`
	}{Policies: []medQueryPolicy{}}
	for _, name := range slices.Sorted(maps.Keys(policies)) {
		for _, application := range slices.Sorted(maps.Keys(policies[name])) {
			policy := policies[name][application]
			value := medQueryPolicy{Interface: types.StringValue(name), Application: types.StringValue(application), Traffic: types.StringValue(policy.Traffic), VLAN: types.Int64Null(), Priority: types.Int64Null(), DSCP: types.Int64Value(policy.DSCP)}
			if policy.Traffic == "tagged" {
				value.VLAN = types.Int64Value(policy.VLAN)
			}
			if policy.Traffic != "untagged" {
				value.Priority = types.Int64Value(policy.Priority)
			}
			state.Policies = append(state.Policies, value)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
