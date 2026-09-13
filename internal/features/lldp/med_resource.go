package lldp

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	tfresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	medResource struct{ device *fastiron.Device }
	medModel    struct {
		ID          types.String `tfsdk:"id"`
		Interface   types.String `tfsdk:"interface"`
		Application types.String `tfsdk:"application"`
		Traffic     types.String `tfsdk:"traffic"`
		VLAN        types.Int64  `tfsdk:"vlan_id"`
		Priority    types.Int64  `tfsdk:"priority"`
		DSCP        types.Int64  `tfsdk:"dscp"`
		Pending     types.Bool   `tfsdk:"persistence_pending"`
	}
)

func NewMEDResource() *medResource { return &medResource{} }

func (r *medResource) Metadata(_ context.Context, req tfresource.MetadataRequest, resp *tfresource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lldp_med_policy"
}

func (r *medResource) Schema(_ context.Context, _ tfresource.SchemaRequest, resp *tfresource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one MED application policy on one Ethernet port. Deletion removes that policy without restoring a prior configuration. Other applications, ports, VLAN membership and LLDP enable settings remain independently owned.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"interface":           schema.StringAttribute{Required: true, Description: "Canonical Ethernet interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"application":         schema.StringAttribute{Required: true, Description: "MED application type.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"traffic":             schema.StringAttribute{Required: true, Description: "untagged, priority-tagged, or tagged."},
		"vlan_id":             schema.Int64Attribute{Optional: true, Description: "VLAN 1 through 4094. Required for tagged traffic; omit for other modes."},
		"priority":            schema.Int64Attribute{Optional: true, Description: "Layer 2 priority, 0 through 7. Required for tagged or priority-tagged traffic; omit for untagged traffic."},
		"dscp":                schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0), Description: "DSCP 0 through 63. Omission resets to zero."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *medResource) Configure(_ context.Context, req tfresource.ConfigureRequest, resp *tfresource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	device, ok := req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
		return
	}
	r.device = device
}

func (r *medResource) ValidateConfig(ctx context.Context, req tfresource.ValidateConfigRequest, resp *tfresource.ValidateConfigResponse) {
	var value medModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &value)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !value.Interface.IsUnknown() && !value.Interface.IsNull() {
		if err := validateInterface(value.Interface.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("interface"), "Invalid MED interface", err.Error())
		}
	}
	if !value.Application.IsUnknown() && !value.Application.IsNull() && !validMEDApplication(value.Application.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("application"), "Invalid MED application", "Use voice, voice-signaling, guest-voice, guest-voice-signaling, softphone-voice, streaming-video, video-conferencing, or video-signaling.")
	}
	for _, field := range []struct {
		name     string
		value    types.Int64
		min, max int64
	}{{"vlan_id", value.VLAN, 1, 4094}, {"priority", value.Priority, 0, 7}, {"dscp", value.DSCP, 0, 63}} {
		if !field.value.IsUnknown() && !field.value.IsNull() && (field.value.ValueInt64() < field.min || field.value.ValueInt64() > field.max) {
			resp.Diagnostics.AddAttributeError(path.Root(field.name), "Invalid MED value", "Value is outside the documented range.")
		}
	}
	if value.Traffic.IsUnknown() || value.Traffic.IsNull() {
		return
	}
	switch value.Traffic.ValueString() {
	case "tagged":
		if value.VLAN.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("vlan_id"), "Missing MED VLAN", "Tagged traffic requires vlan_id.")
		}
		if value.Priority.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("priority"), "Missing MED priority", "Tagged traffic requires priority.")
		}
	case "priority-tagged":
		if !value.VLAN.IsNull() && !value.VLAN.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root("vlan_id"), "Unexpected MED VLAN", "Priority-tagged traffic does not specify vlan_id.")
		}
		if value.Priority.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("priority"), "Missing MED priority", "Priority-tagged traffic requires priority.")
		}
	case "untagged":
		if !value.VLAN.IsNull() && !value.VLAN.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root("vlan_id"), "Unexpected MED VLAN", "Untagged traffic does not specify vlan_id.")
		}
		if !value.Priority.IsNull() && !value.Priority.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root("priority"), "Unexpected MED priority", "Untagged traffic does not specify priority.")
		}
	default:
		resp.Diagnostics.AddAttributeError(path.Root("traffic"), "Invalid MED traffic", "Use untagged, priority-tagged, or tagged.")
	}
}

func (r *medResource) ModifyPlan(ctx context.Context, req tfresource.ModifyPlanRequest, resp *tfresource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan medModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.device == nil || plan.Interface.IsUnknown() || plan.Interface.IsNull() {
		return
	}
	if _, err := readRESTEnabled(ctx, r.device, plan.Interface.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot read MED interface", err.Error())
		return
	}
	if _, _, err := readMED(ctx, r.device); err != nil {
		resp.Diagnostics.AddError("Cannot read MED capability", err.Error())
	}
}

func (m medModel) desired() (config.MEDPolicy, error) {
	if m.Traffic.IsNull() || m.Traffic.IsUnknown() || m.DSCP.IsNull() || m.DSCP.IsUnknown() || m.VLAN.IsUnknown() || m.Priority.IsUnknown() {
		return config.MEDPolicy{}, errors.New("MED policy values must be resolved before applying")
	}
	switch m.Traffic.ValueString() {
	case "tagged":
		if m.VLAN.IsNull() || m.Priority.IsNull() {
			return config.MEDPolicy{}, errors.New("tagged MED traffic requires VLAN and priority")
		}
	case "priority-tagged":
		if !m.VLAN.IsNull() || m.Priority.IsNull() {
			return config.MEDPolicy{}, errors.New("priority-tagged MED traffic requires priority and no VLAN")
		}
	case "untagged":
		if !m.VLAN.IsNull() || !m.Priority.IsNull() {
			return config.MEDPolicy{}, errors.New("untagged MED traffic cannot specify VLAN or priority")
		}
	}
	p := config.MEDPolicy{Traffic: m.Traffic.ValueString(), VLAN: m.VLAN.ValueInt64(), Priority: m.Priority.ValueInt64(), DSCP: m.DSCP.ValueInt64()}
	return p, validateMEDPolicy(p)
}

func (m *medModel) observe(policy *config.MEDPolicy) {
	m.Traffic = types.StringNull()
	m.VLAN = types.Int64Null()
	m.Priority = types.Int64Null()
	m.DSCP = types.Int64Null()
	if policy == nil {
		return
	}
	m.Traffic = types.StringValue(policy.Traffic)
	m.DSCP = types.Int64Value(policy.DSCP)
	if policy.Traffic == "tagged" {
		m.VLAN = types.Int64Value(policy.VLAN)
	}
	if policy.Traffic != "untagged" {
		m.Priority = types.Int64Value(policy.Priority)
	}
}

func (r *medResource) Create(ctx context.Context, req tfresource.CreateRequest, resp *tfresource.CreateResponse) {
	var plan medModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, err := plan.desired()
	if err != nil {
		resp.Diagnostics.AddError("Invalid MED policy", err.Error())
		return
	}
	observed, err := applyMED(ctx, r.device, plan.Interface.ValueString(), plan.Application.ValueString(), &desired)
	if observed.attempted || (observed.verified && observed.policy != nil) {
		plan.ID = types.StringValue("lldp-med|" + plan.Interface.ValueString() + "|" + plan.Application.ValueString())
		plan.Pending = types.BoolValue(err != nil)
		if observed.verified {
			plan.observe(observed.policy)
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot create MED policy", err.Error())
	}
}

func (r *medResource) Update(ctx context.Context, req tfresource.UpdateRequest, resp *tfresource.UpdateResponse) {
	var plan, state medModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, err := plan.desired()
	if err != nil {
		resp.Diagnostics.AddError("Invalid MED policy", err.Error())
		return
	}
	observed, err := applyMED(ctx, r.device, plan.Interface.ValueString(), plan.Application.ValueString(), &desired)
	state.Pending = types.BoolValue(err != nil)
	if observed.verified {
		state.observe(observed.policy)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update MED policy", err.Error())
	}
}

func (r *medResource) Read(ctx context.Context, req tfresource.ReadRequest, resp *tfresource.ReadResponse) {
	var state medModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policies, _, err := readMED(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read MED policy", err.Error())
		return
	}
	p, exists := policies[state.Interface.ValueString()][state.Application.ValueString()]
	// Retain failed removals until a retry can save the verified absence.
	if !exists && !state.Pending.ValueBool() {
		resp.State.RemoveResource(ctx)
		return
	}
	if exists {
		state.observe(&p)
	} else {
		state.observe(nil)
	}
	if state.Pending.IsNull() {
		state.Pending = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *medResource) Delete(ctx context.Context, req tfresource.DeleteRequest, resp *tfresource.DeleteResponse) {
	var state medModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyMED(ctx, r.device, state.Interface.ValueString(), state.Application.ValueString(), nil)
	if err == nil {
		return
	}
	state.Pending = types.BoolValue(true)
	if observed.verified {
		state.observe(observed.policy)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.AddError("Cannot remove MED policy", err.Error())
}

func (r *medResource) ImportState(ctx context.Context, req tfresource.ImportStateRequest, resp *tfresource.ImportStateResponse) {
	parts := strings.Split(req.ID, "|")
	if len(parts) != 3 || parts[0] != "lldp-med" || validateInterface(parts[1]) != nil || !validMEDApplication(parts[2]) {
		resp.Diagnostics.AddError("Invalid MED identity", "Use lldp-med|ethernet <stack>/<slot>/<port>|<application>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("application"), parts[2])...)
}
