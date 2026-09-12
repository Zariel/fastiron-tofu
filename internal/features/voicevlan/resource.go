package voicevlan

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

type interfaceResource struct{ device *fastiron.Device }

type model struct {
	ID        types.String `tfsdk:"id"`
	Interface types.String `tfsdk:"interface"`
	VLANID    types.Int64  `tfsdk:"vlan_id"`
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
	resp.TypeName = req.ProviderTypeName + "_interface_voice_vlan"
}

func (r *interfaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns the local IP voice VLAN on one Ethernet interface. Deletion removes that policy without restoring a prior value. VLAN membership, Ethernet settings, FlexAuth and LLDP-MED remain separately owned. Import with the canonical Ethernet interface name.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical Ethernet interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"interface":           schema.StringAttribute{Required: true, Description: "ethernet <stack>/<slot>/<port>.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"vlan_id":             schema.Int64Attribute{Required: true, Description: "Local voice VLAN identifier, 1 through 4095. This does not create the VLAN or add membership."},
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
			resp.Diagnostics.AddAttributeError(path.Root("interface"), "Invalid voice VLAN interface", err.Error())
		}
	}
	if !config.VLANID.IsNull() && !config.VLANID.IsUnknown() && (config.VLANID.ValueInt64() < 1 || config.VLANID.ValueInt64() > 4095) {
		resp.Diagnostics.AddAttributeError(path.Root("vlan_id"), "Invalid voice VLAN", "Use an identifier between 1 and 4095.")
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
	if _, err := read(ctx, r.device, plan.Interface.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot read voice VLAN capability", err.Error())
	}
}

func (r *interfaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.Interface.ValueString(), plan.VLANID.ValueInt64())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(plan.Interface.ValueString(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply interface voice VLAN", err.Error())
	}
}

func (r *interfaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, plan.Interface.ValueString(), plan.VLANID.ValueInt64())
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(plan.Interface.ValueString(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update interface voice VLAN", err.Error())
	}
}

func (r *interfaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := read(ctx, r.device, state.Interface.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read interface voice VLAN", err.Error())
		return
	}
	// Keep failed deletions addressable until their removal has also been saved.
	if observed.vlanID == 0 && !state.Pending.ValueBool() {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(state.Interface.ValueString(), observed.vlanID, state.Pending.ValueBool()))...)
}

func (r *interfaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state model
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := apply(ctx, r.device, state.Interface.ValueString(), 0)
	if err == nil {
		return
	}
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, resourceState(state.Interface.ValueString(), *observed, true))...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
	resp.Diagnostics.AddError("Cannot remove interface voice VLAN", err.Error())
}

func (r *interfaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if err := validateInterface(req.ID); err != nil {
		resp.Diagnostics.AddError("Invalid voice VLAN identity", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), req.ID)...)
}

func resourceState(name string, id int64, pending bool) model {
	state := model{ID: types.StringValue(name), Interface: types.StringValue(name), VLANID: types.Int64Null(), Pending: types.BoolValue(pending)}
	if id != 0 {
		state.VLANID = types.Int64Value(id)
	}
	return state
}
