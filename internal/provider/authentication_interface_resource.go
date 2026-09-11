package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	authenticationInterfaceResource struct{ device *fastiron.Device }
	authenticationInterfaceModel    struct {
		ID                 types.String `tfsdk:"id"`
		Interface          types.String `tfsdk:"interface"`
		Dot1XEnabled       types.Bool   `tfsdk:"dot1x_enabled"`
		MACEnabled         types.Bool   `tfsdk:"mac_authentication_enabled"`
		PortControl        types.String `tfsdk:"port_control"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *authenticationInterfaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_authentication_interface"
}

func (r *authenticationInterfaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns dot1x and MAC authentication enablement and port-control on one Ethernet interface. Destroy disables both authentication types and resets control to force-authorized. Global authentication settings are separate. Requires allow_aaa_changes.", Attributes: map[string]schema.Attribute{
		"id":                         schema.StringAttribute{Computed: true, Description: "Canonical Ethernet interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"interface":                  schema.StringAttribute{Required: true, Description: "Canonical Ethernet interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"dot1x_enabled":              schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Enable dot1x on this port. Requires global dot1x initialization. Defaults to false."},
		"mac_authentication_enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Enable MAC authentication on this port. Requires global MAC authentication initialization. Defaults to false."},
		"port_control":               schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("force-authorized"), Description: "auto, force-authorized or force-unauthorized. Defaults to force-authorized, which permits traffic without authentication."},
		"persistence_pending":        schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *authenticationInterfaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *authenticationInterfaceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model authenticationInterfaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || model.Interface.IsUnknown() || model.Interface.IsNull() {
		return
	}
	desired := fastiron.AuthenticationInterface{PortControl: "force-authorized"}
	desired.Dot1XEnabled = model.Dot1XEnabled.IsUnknown() || model.Dot1XEnabled.ValueBool()
	if !model.PortControl.IsUnknown() && !model.PortControl.IsNull() {
		desired.PortControl = model.PortControl.ValueString()
	}
	if err := fastiron.ValidateAuthenticationInterface(model.Interface.ValueString(), desired); err != nil {
		resp.Diagnostics.AddError("Invalid authentication interface configuration", err.Error())
	}
}

func (r *authenticationInterfaceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
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
	var plan authenticationInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Interface.IsUnknown() || plan.Dot1XEnabled.IsUnknown() || plan.MACEnabled.IsUnknown() || plan.PortControl.IsUnknown() {
		return
	}
	if err := fastiron.ValidateAuthenticationInterface(plan.Interface.ValueString(), plan.desired()); err != nil {
		resp.Diagnostics.AddError("Invalid authentication interface configuration", err.Error())
		return
	}
	if _, err := r.device.AuthenticationInterface(ctx, plan.Interface.ValueString()); err != nil {
		resp.Diagnostics.AddError("Authentication interface configuration is not supported by the configured switch", err.Error())
	}
}

func (r *authenticationInterfaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan authenticationInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyAuthenticationInterface(ctx, plan.Interface.ValueString(), plan.desired())
	if observed != nil {
		state := authenticationInterfaceState(plan.Interface.ValueString(), *observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply authentication interface configuration", err.Error())
	}
}

func (r *authenticationInterfaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan authenticationInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyAuthenticationInterface(ctx, plan.Interface.ValueString(), plan.desired())
	if observed != nil {
		state := authenticationInterfaceState(plan.Interface.ValueString(), *observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update authentication interface configuration", err.Error())
	}
}

func (r *authenticationInterfaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state authenticationInterfaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.AuthenticationInterface(ctx, state.Interface.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read authentication interface configuration", err.Error())
		return
	}
	current := authenticationInterfaceState(state.Interface.ValueString(), observed)
	if !state.PersistencePending.IsNull() {
		current.PersistencePending = state.PersistencePending
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, current)...)
}

func (r *authenticationInterfaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state authenticationInterfaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.device.ApplyAuthenticationInterface(ctx, state.Interface.ValueString(), fastiron.AuthenticationInterface{PortControl: "force-authorized"})
	if err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot reset authentication interface configuration", err.Error())
	}
}

func (r *authenticationInterfaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if fastiron.ValidateAuthenticationInterface(req.ID, fastiron.AuthenticationInterface{PortControl: "force-authorized"}) != nil {
		resp.Diagnostics.AddError("Invalid authentication interface identity", "Use ethernet <stack>/<slot>/<port>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), req.ID)...)
}

func (m authenticationInterfaceModel) desired() fastiron.AuthenticationInterface {
	return fastiron.AuthenticationInterface{Dot1XEnabled: m.Dot1XEnabled.ValueBool(), MACEnabled: m.MACEnabled.ValueBool(), PortControl: m.PortControl.ValueString()}
}

func authenticationInterfaceState(name string, p fastiron.AuthenticationInterface) authenticationInterfaceModel {
	return authenticationInterfaceModel{ID: types.StringValue(name), Interface: types.StringValue(name), Dot1XEnabled: types.BoolValue(p.Dot1XEnabled), MACEnabled: types.BoolValue(p.MACEnabled), PortControl: types.StringValue(p.PortControl), PersistencePending: types.BoolValue(false)}
}
