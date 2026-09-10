package provider

import (
	"context"
	"errors"
	"strings"

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
	ethernetResource struct{ device *fastiron.Device }
	ethernetModel    struct {
		ID                 types.String `tfsdk:"id"`
		Name               types.String `tfsdk:"name"`
		Port               types.String `tfsdk:"port"`
		PortName           types.String `tfsdk:"port_name"`
		Enabled            types.Bool   `tfsdk:"enabled"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *ethernetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_ethernet"
}

func (r *ethernetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manages a physical Ethernet port's name and administrative enable state. Destroy clears the name and enables the port; it does not delete or broadly reset the interface.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: ethernet <stack>/<slot>/<port>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":                schema.StringAttribute{Computed: true, Description: "Canonical interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"port":                schema.StringAttribute{Required: true, Description: "Physical port in stack/slot/port syntax.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"port_name":           schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "Port description. Omission clears it."},
		"enabled":             schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), Description: "Administrative enable state. Defaults to true."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *ethernetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *ethernetResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model ethernetModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || model.Port.IsUnknown() || model.Port.IsNull() {
		return
	}
	if err := fastiron.ValidateEthernet(model.desired()); err != nil {
		resp.Diagnostics.AddError("Invalid Ethernet configuration", err.Error())
	}
}

func (r *ethernetResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var plan ethernetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Port.IsUnknown() || plan.PortName.IsUnknown() || plan.Enabled.IsUnknown() {
		return
	}
	if err := r.device.CheckEthernet(ctx, plan.desired()); err != nil {
		resp.Diagnostics.AddError("Ethernet configuration is not supported by the configured switch", err.Error())
	}
}

func (r *ethernetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ethernetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyEthernet(ctx, plan.desired())
	if observed != nil {
		state := ethernetState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply Ethernet configuration", err.Error())
	}
}

func (r *ethernetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ethernetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyEthernet(ctx, plan.desired())
	if observed != nil {
		state := ethernetState(*observed)
		state.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update Ethernet configuration", err.Error())
	}
}

func (r *ethernetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ethernetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.Ethernet(ctx, state.Port.ValueString())
	if errors.Is(err, fastiron.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read Ethernet configuration", err.Error())
		return
	}
	current := ethernetState(observed)
	if !state.PersistencePending.IsNull() {
		current.PersistencePending = state.PersistencePending
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, current)...)
}

func (r *ethernetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ethernetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.device.ApplyEthernet(ctx, fastiron.Ethernet{Port: state.Port.ValueString(), Enabled: true})
	if errors.Is(err, fastiron.ErrNotFound) {
		return
	}
	if err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot reset Ethernet configuration", err.Error())
	}
}

func (r *ethernetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	port := strings.TrimPrefix(req.ID, "ethernet ")
	if req.ID != "ethernet "+port || fastiron.ValidateEthernet(fastiron.Ethernet{Port: port}) != nil {
		resp.Diagnostics.AddError("Invalid Ethernet import identity", "Use ethernet <stack>/<slot>/<port>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("port"), port)...)
}

func (m ethernetModel) desired() fastiron.Ethernet {
	return fastiron.Ethernet{Port: m.Port.ValueString(), PortName: m.PortName.ValueString(), Enabled: m.Enabled.ValueBool()}
}

func ethernetState(v fastiron.Ethernet) ethernetModel {
	return ethernetModel{ID: types.StringValue("ethernet " + v.Port), Name: types.StringValue("ethernet " + v.Port), Port: types.StringValue(v.Port), PortName: types.StringValue(v.PortName), Enabled: types.BoolValue(v.Enabled), PersistencePending: types.BoolValue(false)}
}
