package jumbo

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	jumboResource struct{ device *fastiron.Device }
	jumboModel    struct {
		ID      types.String `tfsdk:"id"`
		Active  types.Bool   `tfsdk:"active_enabled"`
		Reload  types.Bool   `tfsdk:"reload_required"`
		Enabled types.Bool   `tfsdk:"enabled"`
		Pending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func NewResource() *jumboResource { return &jumboResource{} }

var (
	_ resource.ResourceWithConfigure   = (*jumboResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*jumboResource)(nil)
	_ resource.ResourceWithImportState = (*jumboResource)(nil)
)

func (r *jumboResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_jumbo"
}

func (r *jumboResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns configured global jumbo-frame support. Declare one per switch. Deletion configures jumbo mode off; save and reload to activate the change. Reloads are initiated separately. Per-interface MTUs remain independently owned. Import with global.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Singleton identity: global.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"enabled":             schema.BoolAttribute{Required: true, Description: "Whether global jumbo-frame support is configured."},
		"active_enabled":      schema.BoolAttribute{Computed: true, Description: "Switch-reported active jumbo mode. Saved configuration takes effect after a separately initiated reload."},
		"reload_required":     schema.BoolAttribute{Computed: true, Description: "Configured and active jumbo modes differ. Save configuration before reloading the switch."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *jumboResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *jumboResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	if r.device == nil {
		return
	}
	if _, err := read(ctx, r.device); err != nil {
		resp.Diagnostics.AddError("Cannot read jumbo capability", err.Error())
	}
}

func (r *jumboResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan jumboModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.Enabled.ValueBool())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, jumboState(*observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply jumbo configuration", err.Error())
	}
}

func (r *jumboResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan jumboModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.Enabled.ValueBool())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, jumboState(*observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update jumbo configuration", err.Error())
	}
}

func (r *jumboResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state jumboModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := read(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read jumbo configuration", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, jumboState(observed, state.Pending.ValueBool()))...)
}

func (r *jumboResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	observed, err := apply(ctx, r.device, false)
	if err == nil {
		return
	}
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, jumboState(*observed, true))...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
	resp.Diagnostics.AddError("Cannot reset jumbo configuration", err.Error())
}

func (r *jumboResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "global" {
		resp.Diagnostics.AddError("Invalid jumbo identity", "Use global to import the switch's global jumbo configuration.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "global")...)
}

func jumboState(observed observation, pending bool) jumboModel {
	return jumboModel{ID: types.StringValue("global"), Enabled: types.BoolValue(observed.enabled), Active: types.BoolValue(observed.active), Reload: types.BoolValue(observed.enabled != observed.active), Pending: types.BoolValue(pending)}
}
