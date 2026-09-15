package poe

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/config"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	Resource struct{ device *fastiron.Device }
	model    struct {
		Priority           types.Int64  `tfsdk:"priority"`
		PowerByClass       types.Int64  `tfsdk:"power_by_class"`
		PowerLimit         types.Int64  `tfsdk:"power_limit_milliwatts"`
		ID                 types.String `tfsdk:"id"`
		Interface          types.String `tfsdk:"interface"`
		Enabled            types.Bool   `tfsdk:"enabled"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *Resource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_poe"
}

func (r *Resource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns PoE enable state, priority and allocation on one Ethernet interface. Omission and destroy restore enabled=true, priority=3 and class-based allocation with class=0. Power measurements are available from the PoE data source and do not cause configuration drift.", Attributes: map[string]schema.Attribute{
		"id":                     schema.StringAttribute{Computed: true, Description: "Canonical identity: poe|ethernet <stack>/<slot>/<port>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"interface":              schema.StringAttribute{Required: true, Description: "Canonical Ethernet interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"enabled":                schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), Description: "Administrative enable state. Defaults to true."},
		"priority":               schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(3), Description: "Power priority, 1 (highest) to 3 (lowest). Defaults to 3."},
		"power_by_class":         schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0), Description: "Allocation class, 0 through 4. Defaults to 0. Must be zero when an explicit power limit is configured."},
		"power_limit_milliwatts": schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(0), Description: "Explicit allocation limit in milliwatts, 1000 through 95000 subject to port capabilities. Zero (default) selects class-based allocation."},
		"persistence_pending":    schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *Resource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model model
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !model.Interface.IsUnknown() && !model.Interface.IsNull() {
		if err := validateInterface(model.Interface.ValueString()); err != nil {
			resp.Diagnostics.AddError("Invalid PoE configuration", err.Error())
		}
	}
	if err := validatePolicy(model.policy()); err != nil {
		resp.Diagnostics.AddError("Invalid PoE configuration", err.Error())
	}
}

func (m model) policy() config.PoEPolicy {
	policy := config.PoEPolicy{Enabled: true, Priority: 3}
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		policy.Enabled = m.Enabled.ValueBool()
	}
	if !m.Priority.IsNull() && !m.Priority.IsUnknown() {
		policy.Priority = m.Priority.ValueInt64()
	}
	policy.PowerByClass = m.PowerByClass.ValueInt64()
	policy.PowerLimitMilliwatts = m.PowerLimit.ValueInt64()
	return policy
}

func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Interface.IsUnknown() || plan.Enabled.IsUnknown() {
		return
	}
	if _, err := readPort(ctx, r.device, plan.Interface.ValueString()); err != nil {
		resp.Diagnostics.AddError("PoE configuration is not supported by the configured switch", err.Error())
	}
}

func (r *Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyPort(ctx, r.device, plan.Interface.ValueString(), plan.policy())
	if observed != nil {
		state := resourceState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply PoE configuration", err.Error())
	}
}

func (r *Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyPort(ctx, r.device, plan.Interface.ValueString(), plan.policy())
	if observed != nil {
		state := resourceState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update PoE configuration", err.Error())
	}
}

func (r *Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := readPort(ctx, r.device, state.Interface.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read PoE configuration", err.Error())
		return
	}
	current := resourceState(observed)
	if !state.PersistencePending.IsNull() {
		current.PersistencePending = state.PersistencePending
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, current)...)
}

func (r *Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := applyPort(ctx, r.device, state.Interface.ValueString(), config.PoEPolicy{Enabled: true, Priority: 3})
	if err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot reset PoE configuration", err.Error())
	}
}

func (r *Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name := strings.TrimPrefix(req.ID, "poe|")
	if req.ID != "poe|"+name || validateInterface(name) != nil {
		resp.Diagnostics.AddError("Invalid PoE identity", "Use poe|ethernet <stack>/<slot>/<port>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), name)...)
}

func resourceState(p port) model {
	return model{Priority: types.Int64Value(p.Priority), PowerByClass: types.Int64Value(p.PowerByClass), PowerLimit: types.Int64Value(p.PowerLimitMilliwatts), ID: types.StringValue("poe|" + p.Name), Interface: types.StringValue(p.Name), Enabled: types.BoolValue(p.Enabled), PersistencePending: types.BoolValue(false)}
}
