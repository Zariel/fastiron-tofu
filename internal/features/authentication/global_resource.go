package authentication

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type GlobalResource struct{ device *fastiron.Device }

type globalModel struct {
	ID                 types.String `tfsdk:"id"`
	PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	Dot1XEnabled       types.Bool   `tfsdk:"dot1x_enabled"`
	MACEnabled         types.Bool   `tfsdk:"mac_authentication_enabled"`
	AuthOrder          types.String `tfsdk:"auth_order"`
	DefaultVLAN        types.Int64  `tfsdk:"auth_default_vlan"`
	RestrictedVLAN     types.Int64  `tfsdk:"restricted_vlan"`
	CriticalVLAN       types.Int64  `tfsdk:"critical_vlan"`
	VoiceVLAN          types.Int64  `tfsdk:"voice_vlan"`
	MaxSessions        types.Int64  `tfsdk:"max_sessions"`
	Reauthentication   types.Bool   `tfsdk:"re_authentication"`
	MACDot1XDisable    types.Bool   `tfsdk:"mac_dot1x_disable"`
	MACDot1XOverride   types.Bool   `tfsdk:"mac_dot1x_override"`
	FailureAction      types.String `tfsdk:"failure_action"`
	TimeoutAction      types.String `tfsdk:"timeout_action"`
}

func (r *GlobalResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_authentication"
}

func (r *GlobalResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns global FlexAuth enablement, VLANs, authentication order, basic actions and MAC options. Port settings, guest VLAN and additional timers are separate. Destroy resets owned fields to defaults. Requires allow_aaa_changes.", Attributes: map[string]schema.Attribute{
		"id":                         schema.StringAttribute{Computed: true, Description: "Canonical identity: authentication.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"persistence_pending":        schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
		"dot1x_enabled":              schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Whether dot1x is enabled globally."},
		"mac_authentication_enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Whether MAC authentication is enabled globally."},
		"auth_order":                 schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("dot1x mac-auth"), Description: "Authentication sequence: dot1x mac-auth (default) or mac-auth dot1x."},
		"auth_default_vlan":          schema.Int64Attribute{Optional: true, Description: "Authentication default VLAN; null when unconfigured."},
		"restricted_vlan":            schema.Int64Attribute{Optional: true, Description: "Restricted VLAN; null when unconfigured."},
		"critical_vlan":              schema.Int64Attribute{Optional: true, Description: "Critical VLAN; null when unconfigured."},
		"voice_vlan":                 schema.Int64Attribute{Optional: true, Description: "Voice VLAN; null when unconfigured."},
		"max_sessions":               schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(2), Description: "Global maximum authentication sessions; defaults to 2."},
		"re_authentication":          schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Whether periodic reauthentication is enabled."},
		"mac_dot1x_disable":          schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Skip dot1x after successful MAC authentication when MAC authentication is first."},
		"mac_dot1x_override":         schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Attempt dot1x after failed MAC authentication with MAC-first order and restricted-VLAN failure handling."},
		"failure_action":             schema.StringAttribute{Optional: true, Description: "restricted-vlan, or unset to block failed authentication. Voice VLAN variants are not writable through this resource."},
		"timeout_action":             schema.StringAttribute{Optional: true, Description: "success, failure, critical-vlan, or unset to remove the configured timeout action."},
	}}
}

func (r *GlobalResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *GlobalResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model globalModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || !model.known() {
		return
	}
	if err := model.validate(); err != nil {
		resp.Diagnostics.AddError("Invalid global authentication configuration", err.Error())
	}
}

func (r *GlobalResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if r.device != nil {
		if err := r.device.CheckAAAChanges(); err != nil {
			resp.Diagnostics.AddError("AAA changes disabled", err.Error())
			return
		}
	}
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan globalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !plan.known() {
		return
	}
	if err := plan.validate(); err != nil {
		resp.Diagnostics.AddError("Invalid global authentication configuration", err.Error())
		return
	}
	if !r.device.RESTCONFEnabled() {
		resp.Diagnostics.AddError("Global authentication is not supported by the configured transport", "RESTCONF is required for global authentication configuration.")
		return
	}
	if _, err := readGlobal(ctx, r.device); err != nil {
		resp.Diagnostics.AddError("Cannot read global authentication", err.Error())
	}
}

func (r *GlobalResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan globalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyGlobal(ctx, r.device, plan.desired())
	if observed != nil {
		state := globalState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply global authentication configuration", err.Error())
	}
}

func (r *GlobalResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan globalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyGlobal(ctx, r.device, plan.desired())
	if observed != nil {
		state := globalState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply global authentication configuration", err.Error())
	}
}

func (r *GlobalResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state globalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := readGlobal(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read global authentication", err.Error())
		return
	}
	current := globalState(observed)
	if !state.PersistencePending.IsNull() {
		current.PersistencePending = state.PersistencePending
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, current)...)
}

func (r *GlobalResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	_, err := applyGlobal(ctx, r.device, globalConfig{AuthOrder: "dot1x mac-auth", MaxSessions: 2})
	if err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot reset global authentication", err.Error())
	}
}

func (r *GlobalResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "authentication" {
		resp.Diagnostics.AddError("Invalid global authentication identity", "Use authentication.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (m globalModel) known() bool {
	for _, value := range []attr.Value{m.Dot1XEnabled, m.MACEnabled, m.AuthOrder, m.DefaultVLAN, m.RestrictedVLAN, m.CriticalVLAN, m.VoiceVLAN, m.MaxSessions, m.Reauthentication, m.MACDot1XDisable, m.MACDot1XOverride, m.FailureAction, m.TimeoutAction} {
		if value.IsUnknown() {
			return false
		}
	}
	return true
}

func (m globalModel) desired() globalConfig {
	p := globalConfig{
		Dot1XEnabled:     m.Dot1XEnabled.ValueBool(),
		MACEnabled:       m.MACEnabled.ValueBool(),
		AuthOrder:        m.AuthOrder.ValueString(),
		DefaultVLAN:      m.DefaultVLAN.ValueInt64(),
		RestrictedVLAN:   m.RestrictedVLAN.ValueInt64(),
		CriticalVLAN:     m.CriticalVLAN.ValueInt64(),
		VoiceVLAN:        m.VoiceVLAN.ValueInt64(),
		MaxSessions:      m.MaxSessions.ValueInt64(),
		Reauthentication: m.Reauthentication.ValueBool(),
		MACDot1XDisable:  m.MACDot1XDisable.ValueBool(),
		MACDot1XOverride: m.MACDot1XOverride.ValueBool(),
		FailureAction:    m.FailureAction.ValueString(),
		TimeoutAction:    m.TimeoutAction.ValueString(),
	}
	if m.AuthOrder.IsNull() {
		p.AuthOrder = "dot1x mac-auth"
	}
	if m.MaxSessions.IsNull() {
		p.MaxSessions = 2
	}
	return p
}

func globalState(p globalConfig) globalModel {
	m := globalModel{
		ID: types.StringValue("authentication"), PersistencePending: types.BoolValue(false),
		Dot1XEnabled:     types.BoolValue(p.Dot1XEnabled),
		MACEnabled:       types.BoolValue(p.MACEnabled),
		AuthOrder:        types.StringValue(p.AuthOrder),
		DefaultVLAN:      types.Int64Null(),
		RestrictedVLAN:   types.Int64Null(),
		CriticalVLAN:     types.Int64Null(),
		VoiceVLAN:        types.Int64Null(),
		MaxSessions:      types.Int64Value(p.MaxSessions),
		Reauthentication: types.BoolValue(p.Reauthentication),
		MACDot1XDisable:  types.BoolValue(p.MACDot1XDisable),
		MACDot1XOverride: types.BoolValue(p.MACDot1XOverride),
		FailureAction:    types.StringNull(),
		TimeoutAction:    types.StringNull(),
	}
	if p.DefaultVLAN != 0 {
		m.DefaultVLAN = types.Int64Value(p.DefaultVLAN)
	}
	if p.RestrictedVLAN != 0 {
		m.RestrictedVLAN = types.Int64Value(p.RestrictedVLAN)
	}
	if p.CriticalVLAN != 0 {
		m.CriticalVLAN = types.Int64Value(p.CriticalVLAN)
	}
	if p.VoiceVLAN != 0 {
		m.VoiceVLAN = types.Int64Value(p.VoiceVLAN)
	}
	if p.FailureAction != "" {
		m.FailureAction = types.StringValue(p.FailureAction)
	}
	if p.TimeoutAction != "" {
		m.TimeoutAction = types.StringValue(p.TimeoutAction)
	}
	return m
}

func (m globalModel) validate() error {
	for _, id := range []types.Int64{m.DefaultVLAN, m.RestrictedVLAN, m.CriticalVLAN, m.VoiceVLAN} {
		if !id.IsNull() && (id.ValueInt64() < 1 || id.ValueInt64() > 4094) {
			return errors.New("configured authentication VLAN IDs must be between 1 and 4094")
		}
	}
	for _, action := range []types.String{m.FailureAction, m.TimeoutAction} {
		if !action.IsNull() && action.ValueString() == "" {
			return errors.New("omit an authentication action instead of setting an empty string")
		}
	}
	return validateGlobal(m.desired())
}
