package dscptrust

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
	ID        types.String `tfsdk:"id"`
	Interface types.String `tfsdk:"interface"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	Pending   types.Bool   `tfsdk:"persistence_pending"`
}

func NewResource() *interfaceResource { return &interfaceResource{} }

var (
	_ resource.ResourceWithConfigure      = (*interfaceResource)(nil)
	_ resource.ResourceWithValidateConfig = (*interfaceResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*interfaceResource)(nil)
	_ resource.ResourceWithImportState    = (*interfaceResource)(nil)
)

func (r *interfaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_dscp_trust"
}

func (r *interfaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns DSCP trust on one interface where the RESTCONF endpoint is available. Deletion disables trust. Interface settings, QoS mappings and LAG membership remain separately owned. Import with the canonical interface name.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical Ethernet or LAG interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"interface":           schema.StringAttribute{Required: true, Description: "ethernet <stack>/<slot>/<port> or lag <id>.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"enabled":             schema.BoolAttribute{Required: true, Description: "Whether this interface is configured to honor Layer 3 DSCP-based QoS instead of the default Layer 2 CoS value."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
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
			resp.Diagnostics.AddAttributeError(path.Root("interface"), "Invalid DSCP trust interface", err.Error())
		}
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
		resp.Diagnostics.AddError("Cannot read DSCP trust capability", err.Error())
		return
	}
	if err := observed.validate(observed.enabled || plan.Enabled.ValueBool()); err != nil {
		resp.Diagnostics.AddError("Incompatible DSCP trust configuration", err.Error())
	}
}

func (r *interfaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.Interface.ValueString(), plan.Enabled.ValueBool())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(plan.Interface.ValueString(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply interface DSCP trust", err.Error())
	}
}

func (r *interfaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.Interface.ValueString(), plan.Enabled.ValueBool())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(plan.Interface.ValueString(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update interface DSCP trust", err.Error())
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
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), false)...)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read interface DSCP trust", err.Error())
		return
	}
	if err := observed.validate(observed.enabled); err != nil {
		resp.Diagnostics.AddError("Incompatible DSCP trust configuration", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(state.Interface.ValueString(), observed.enabled, state.Pending.ValueBool()))...)
}

func (r *interfaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, state.Interface.ValueString(), false)
	if err == nil {
		return
	}
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(state.Interface.ValueString(), *observed, true))...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
	resp.Diagnostics.AddError("Cannot remove interface DSCP trust", err.Error())
}

func (r *interfaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if err := validateInterface(req.ID); err != nil {
		resp.Diagnostics.AddError("Invalid DSCP trust identity", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), req.ID)...)
}

func resourceState(name string, enabled, pending bool) model {
	return model{ID: types.StringValue(name), Interface: types.StringValue(name), Enabled: types.BoolValue(enabled), Pending: types.BoolValue(pending)}
}
