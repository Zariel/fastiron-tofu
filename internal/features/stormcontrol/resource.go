package stormcontrol

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type interfaceResource struct{ device *fastiron.Device }

type model struct {
	ID             types.String `tfsdk:"id"`
	Interface      types.String `tfsdk:"interface"`
	Unit           types.String `tfsdk:"unit"`
	Broadcast      types.Int64  `tfsdk:"broadcast_limit"`
	Multicast      types.Int64  `tfsdk:"multicast_limit"`
	UnknownUnicast types.Int64  `tfsdk:"unknown_unicast_limit"`
	Pending        types.Bool   `tfsdk:"persistence_pending"`
}

func NewResource() *interfaceResource { return &interfaceResource{} }

var (
	_ resource.ResourceWithConfigure      = (*interfaceResource)(nil)
	_ resource.ResourceWithValidateConfig = (*interfaceResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*interfaceResource)(nil)
	_ resource.ResourceWithImportState    = (*interfaceResource)(nil)
)

func (r *interfaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_storm_control"
}

func (r *interfaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns all storm-control rate limits on one Ethernet or LAG interface. At least one limit is required. Deletion removes the policy; changing units temporarily removes all limits. Import with the canonical interface name.", Attributes: map[string]schema.Attribute{
		"id":                    schema.StringAttribute{Computed: true, Description: "Canonical Ethernet or LAG interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"interface":             schema.StringAttribute{Required: true, Description: "ethernet <stack>/<slot>/<port> or lag <id>.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"unit":                  schema.StringAttribute{Required: true, Description: "Shared rate unit: pps or kbps."},
		"broadcast_limit":       schema.Int64Attribute{Optional: true, Description: "Broadcast rate limit. Omit to disable this class."},
		"multicast_limit":       schema.Int64Attribute{Optional: true, Description: "Multicast rate limit. Omit to disable this class."},
		"unknown_unicast_limit": schema.Int64Attribute{Optional: true, Description: "Unknown-unicast rate limit. Omit to disable this class."},
		"persistence_pending":   schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *interfaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *interfaceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config model
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.Interface.IsNull() && !config.Interface.IsUnknown() {
		if err := validateInterface(config.Interface.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("interface"), "Invalid storm control interface", err.Error())
		}
	}
	if config.Unit.IsUnknown() || config.Broadcast.IsUnknown() || config.Multicast.IsUnknown() || config.UnknownUnicast.IsUnknown() {
		return
	}
	desired := config.policy()
	if len(desired.limits) == 0 {
		resp.Diagnostics.AddError("Missing storm limits", "Configure at least one traffic-class limit.")
		return
	}
	if err := desired.validate(); err != nil {
		resp.Diagnostics.AddError("Invalid storm policy", err.Error())
	}
}

func (r *interfaceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Interface.IsUnknown() || r.device == nil {
		return
	}
	observed, err := read(ctx, r.device, plan.Interface.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read storm control capability", err.Error())
		return
	}
	if err := observed.writable(); err != nil {
		resp.Diagnostics.AddError("Unsupported storm options", err.Error())
	}
}

func (r *interfaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.Interface.ValueString(), plan.policy())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(plan.Interface.ValueString(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply interface storm control", err.Error())
	}
}

func (r *interfaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.Interface.ValueString(), plan.policy())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(plan.Interface.ValueString(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update interface storm control", err.Error())
	}
}

func (r *interfaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := read(ctx, r.device, state.Interface.ValueString())
	if errors.Is(err, fastiron.ErrNotFound) {
		if !state.Pending.ValueBool() {
			resp.State.RemoveResource(ctx)
			return
		}
		// Keep failed removal addressable until its save has been retried.
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(state.Interface.ValueString(), policy{}, true))...)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read interface storm control", err.Error())
		return
	}
	if err := observed.writable(); err != nil {
		resp.Diagnostics.AddError("Unsupported storm options", err.Error())
		return
	}
	if len(observed.policy.limits) == 0 && !state.Pending.ValueBool() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(state.Interface.ValueString(), observed.policy, state.Pending.ValueBool()))...)
}

func (r *interfaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, state.Interface.ValueString(), policy{})
	if err == nil {
		return
	}
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(state.Interface.ValueString(), *observed, true))...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
	resp.Diagnostics.AddError("Cannot remove interface storm control", err.Error())
}

func (r *interfaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if err := validateInterface(req.ID); err != nil {
		resp.Diagnostics.AddError("Invalid storm control identity", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), req.ID)...)
}

func resourceState(name string, observed policy, pending bool) model {
	state := model{ID: types.StringValue(name), Interface: types.StringValue(name), Unit: types.StringNull(), Broadcast: types.Int64Null(), Multicast: types.Int64Null(), UnknownUnicast: types.Int64Null(), Pending: types.BoolValue(pending)}
	if observed.unit != "" {
		state.Unit = types.StringValue(observed.unit)
	}
	for class, rate := range observed.limits {
		switch class {
		case "broadcast":
			state.Broadcast = types.Int64Value(rate)
		case "multicast":
			state.Multicast = types.Int64Value(rate)
		case "unknown-unicast":
			state.UnknownUnicast = types.Int64Value(rate)
		}
	}
	return state
}

func (m model) policy() policy {
	desired := policy{unit: m.Unit.ValueString(), limits: map[string]int64{}}
	for class, rate := range map[string]types.Int64{"broadcast": m.Broadcast, "multicast": m.Multicast, "unknown-unicast": m.UnknownUnicast} {
		if !rate.IsNull() && !rate.IsUnknown() {
			desired.limits[class] = rate.ValueInt64()
		}
	}
	return desired
}
