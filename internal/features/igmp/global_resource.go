package igmp

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	globalResource struct{ device *fastiron.Device }
	globalModel    struct {
		ID      types.String `tfsdk:"id"`
		Mode    types.String `tfsdk:"querier_mode"`
		Version types.Int64  `tfsdk:"version"`
		Pending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func NewGlobalResource() *globalResource { return &globalResource{} }

var (
	_ resource.ResourceWithConfigure      = (*globalResource)(nil)
	_ resource.ResourceWithValidateConfig = (*globalResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*globalResource)(nil)
	_ resource.ResourceWithImportState    = (*globalResource)(nil)
)

func (r *globalResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_igmp_snooping"
}

func (r *globalResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns global IGMP snooping mode and version. Declare one per switch. Deletion restores disabled mode and version 2 while preserving VLAN overrides and other global multicast settings. Import with global.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Singleton identity: global.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"querier_mode":        schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("disabled"), Description: "Global active, passive or disabled mode. Disabled clears explicit global enablement; VLAN overrides remain independent."},
		"version":             schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(2), Description: "Global IGMP version, 2 or 3. Defaults to 2. VLAN and port overrides take precedence."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *globalResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *globalResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model globalModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !model.Mode.IsNull() && !model.Mode.IsUnknown() {
		if err := validateGlobal(settings{Mode: model.Mode.ValueString(), Version: 2}); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("querier_mode"), "Invalid global IGMP mode", err.Error())
		}
	}
	if !model.Version.IsNull() && !model.Version.IsUnknown() {
		if err := validateGlobal(settings{Mode: "disabled", Version: model.Version.ValueInt64()}); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("version"), "Invalid global IGMP version", err.Error())
		}
	}
}

func (r *globalResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	if r.device == nil {
		return
	}
	if _, err := readGlobal(ctx, r.device); err != nil {
		resp.Diagnostics.AddError("Cannot read global IGMP capability", err.Error())
	}
}

func (r *globalResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan globalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := applyGlobal(ctx, r.device, settings{Mode: plan.Mode.ValueString(), Version: plan.Version.ValueInt64()})
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, globalState(*observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply global IGMP policy", err.Error())
	}
}

func (r *globalResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan globalModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := applyGlobal(ctx, r.device, settings{Mode: plan.Mode.ValueString(), Version: plan.Version.ValueInt64()})
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, globalState(*observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update global IGMP policy", err.Error())
	}
}

func (r *globalResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state globalModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := readGlobal(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read global IGMP policy", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, globalState(observed.settings, state.Pending.ValueBool()))...)
}

func (r *globalResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	observed, err := applyGlobal(ctx, r.device, settings{Mode: "disabled", Version: 2})
	if err == nil {
		return
	}
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, globalState(*observed, true))...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
	resp.Diagnostics.AddError("Cannot reset global IGMP policy", err.Error())
}

func (r *globalResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "global" {
		resp.Diagnostics.AddError("Invalid global IGMP identity", "Use global to import the switch's global IGMP snooping policy.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "global")...)
}

func globalState(observed settings, pending bool) globalModel {
	return globalModel{ID: types.StringValue("global"), Mode: types.StringValue(observed.Mode), Version: types.Int64Value(observed.Version), Pending: types.BoolValue(pending)}
}
