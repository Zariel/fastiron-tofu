package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	stpInterfaceResource struct{ device *fastiron.Device }
	stpInterfaceModel    struct {
		ID                 types.String `tfsdk:"id"`
		Interface          types.String `tfsdk:"interface"`
		AdminEdge          types.Bool   `tfsdk:"admin_edge"`
		BPDUGuard          types.Bool   `tfsdk:"bpdu_guard"`
		RootGuard          types.Bool   `tfsdk:"root_guard"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *stpInterfaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_spanning_tree_interface"
}

func (r *stpInterfaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns admin-edge, BPDU guard and root guard on one Ethernet interface. Omission and destroy reset these options to false; other interface settings are preserved.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical Ethernet interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"interface":           schema.StringAttribute{Required: true, Description: "Canonical Ethernet interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"admin_edge":          schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Configure the interface as an RSTP edge port. Defaults to false."},
		"bpdu_guard":          schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Enable BPDU guard. Defaults to false."},
		"root_guard":          schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Enable root protection. Defaults to false."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *stpInterfaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *stpInterfaceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model stpInterfaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || model.Interface.IsUnknown() || model.Interface.IsNull() {
		return
	}
	if err := fastiron.ValidateSTPInterface(model.Interface.ValueString()); err != nil {
		resp.Diagnostics.AddError("Invalid spanning-tree interface configuration", err.Error())
	}
}

func (r *stpInterfaceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan stpInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Interface.IsUnknown() || plan.AdminEdge.IsUnknown() || plan.BPDUGuard.IsUnknown() || plan.RootGuard.IsUnknown() {
		return
	}
	if _, err := r.device.STPInterface(ctx, plan.Interface.ValueString()); err != nil {
		resp.Diagnostics.AddError("Spanning-tree interface configuration is not supported by the configured switch", err.Error())
	}
}

func (r *stpInterfaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan stpInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplySTPInterface(ctx, plan.Interface.ValueString(), plan.desired())
	if observed != nil {
		state := stpInterfaceState(plan.Interface.ValueString(), *observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply spanning-tree interface configuration", err.Error())
	}
}

func (r *stpInterfaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan stpInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplySTPInterface(ctx, plan.Interface.ValueString(), plan.desired())
	if observed != nil {
		state := stpInterfaceState(plan.Interface.ValueString(), *observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update spanning-tree interface configuration", err.Error())
	}
}

func (r *stpInterfaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state stpInterfaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.STPInterface(ctx, state.Interface.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read spanning-tree interface configuration", err.Error())
		return
	}
	current := stpInterfaceState(state.Interface.ValueString(), observed)
	if !state.PersistencePending.IsNull() {
		current.PersistencePending = state.PersistencePending
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, current)...)
}

func (r *stpInterfaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state stpInterfaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.device.ApplySTPInterface(ctx, state.Interface.ValueString(), fastiron.STPInterface{})
	if err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot reset spanning-tree interface configuration", err.Error())
	}
}

func (r *stpInterfaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if fastiron.ValidateSTPInterface(req.ID) != nil {
		resp.Diagnostics.AddError("Invalid spanning-tree interface identity", "Use ethernet <stack>/<slot>/<port>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), req.ID)...)
}

func (m stpInterfaceModel) desired() fastiron.STPInterface {
	return fastiron.STPInterface{AdminEdge: m.AdminEdge.ValueBool(), BPDUGuard: m.BPDUGuard.ValueBool(), RootGuard: m.RootGuard.ValueBool()}
}

func stpInterfaceState(name string, p fastiron.STPInterface) stpInterfaceModel {
	return stpInterfaceModel{ID: types.StringValue(name), Interface: types.StringValue(name), AdminEdge: types.BoolValue(p.AdminEdge), BPDUGuard: types.BoolValue(p.BPDUGuard), RootGuard: types.BoolValue(p.RootGuard), PersistencePending: types.BoolValue(false)}
}
