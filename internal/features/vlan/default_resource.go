package vlan

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
	defaultResource struct{ device *fastiron.Device }
	defaultModel    struct {
		ID                 types.String `tfsdk:"id"`
		VLANID             types.Int64  `tfsdk:"vlan_id"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func NewDefaultResource() *defaultResource { return &defaultResource{} }

var (
	_ resource.ResourceWithConfigure      = (*defaultResource)(nil)
	_ resource.ResourceWithValidateConfig = (*defaultResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*defaultResource)(nil)
	_ resource.ResourceWithImportState    = (*defaultResource)(nil)
)

func (r *defaultResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_default_vlan"
}

func (r *defaultResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Selects the global default VLAN by renumbering its existing configuration. Declare only one resource per switch. The target ID must be unused. Deletion resets the default to VLAN 1 and requires that ID to be unused. Import with default.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Singleton identity: default.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"vlan_id":             schema.Int64Attribute{Required: true, Description: "Default VLAN identifier, 1 through 4095. Changing it preserves the default VLAN's existing properties."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *defaultResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *defaultResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model defaultModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || model.VLANID.IsNull() || model.VLANID.IsUnknown() {
		return
	}
	if id := model.VLANID.ValueInt64(); id < 1 || id > 4095 {
		resp.Diagnostics.AddAttributeError(path.Root("vlan_id"), "Invalid default VLAN", "Use an identifier between 1 and 4095.")
	}
}

func (r *defaultResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
}

func (r *defaultResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan defaultModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := applyDefault(ctx, r.device, plan.VLANID.ValueInt64())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, defaultModel{ID: types.StringValue("default"), VLANID: types.Int64Value(*observed), PersistencePending: types.BoolValue(err != nil)})...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot select default VLAN", err.Error())
	}
}

func (r *defaultResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan defaultModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := applyDefault(ctx, r.device, plan.VLANID.ValueInt64())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, defaultModel{ID: types.StringValue("default"), VLANID: types.Int64Value(*observed), PersistencePending: types.BoolValue(err != nil)})...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot select default VLAN", err.Error())
	}
}

func (r *defaultResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state defaultModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := readDefault(ctx, r.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read default VLAN", err.Error())
		return
	}

	state.ID = types.StringValue("default")
	state.VLANID = types.Int64Value(observed.id)
	if state.PersistencePending.IsNull() {
		state.PersistencePending = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *defaultResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	observed, err := applyDefault(ctx, r.device, 1)
	if err == nil {
		return
	}
	if observed != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vlan_id"), *observed)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
	resp.Diagnostics.AddError("Cannot reset default VLAN", err.Error())
}

func (r *defaultResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "default" {
		resp.Diagnostics.AddError("Invalid default VLAN identity", "Use default to import the switch's global default VLAN selection.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "default")...)
}
