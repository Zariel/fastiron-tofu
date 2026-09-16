package lag

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type interfaceResource struct{ device *fastiron.Device }

type interfaceModel struct {
	ID                 types.String `tfsdk:"id"`
	LAGID              types.Int64  `tfsdk:"lag_id"`
	PortName           types.String `tfsdk:"port_name"`
	Enabled            types.Bool   `tfsdk:"enabled"`
	PersistencePending types.Bool   `tfsdk:"persistence_pending"`
}

func NewInterfaceResource() *interfaceResource { return &interfaceResource{} }

var (
	_ resource.ResourceWithConfigure      = (*interfaceResource)(nil)
	_ resource.ResourceWithImportState    = (*interfaceResource)(nil)
	_ resource.ResourceWithValidateConfig = (*interfaceResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*interfaceResource)(nil)
)

func (r *interfaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_interface_lag"
}

func (r *interfaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns the description and administrative state of an existing LAG interface. Omission and destruction clear its description and enable it. Administrative transitions affect all members using native behavior; individual member names, membership and other interface settings remain independent. Requires RESTCONF and SSH observation.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity, lag <id>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"lag_id":              schema.Int64Attribute{Required: true, Description: "Positive identifier of the existing aggregate. Changing it replaces the interface policy.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"port_name":           schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "Virtual-interface description. Omission clears it; individual member names remain separate."},
		"enabled":             schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), Description: "Virtual-interface administrative state. Transitions enable or disable all members without restoring prior member state. Defaults to true."},
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
	var model interfaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || model.LAGID.IsUnknown() || model.LAGID.IsNull() || model.PortName.IsUnknown() {
		return
	}

	if err := validateInterface(model.LAGID.ValueInt64(), model.desired()); err != nil {
		resp.Diagnostics.AddError("Invalid LAG interface configuration", err.Error())
	}
}

func (r *interfaceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}

	// A parent can be created in this plan while its numeric identifier is already known.
	// Defer parent existence checks until apply, after dependency ordering takes effect.
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
}

func (r *interfaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan interfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := applyInterface(ctx, r.device, plan.LAGID.ValueInt64(), plan.desired(), true)
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, interfaceState(plan.LAGID.ValueInt64(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply LAG interface configuration", err.Error())
	}
}

func (r *interfaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan interfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := applyInterface(ctx, r.device, plan.LAGID.ValueInt64(), plan.desired(), true)
	if observed != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, interfaceState(plan.LAGID.ValueInt64(), *observed, err != nil))...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update LAG interface configuration", err.Error())
	}
}

func (r *interfaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state interfaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := readInterface(ctx, r.device, state.LAGID.ValueInt64())
	if errors.Is(err, fastiron.ErrNotFound) {
		if !state.PersistencePending.ValueBool() {
			resp.State.RemoveResource(ctx)
			return
		}
		// Keep a failed deletion addressable until its persistence retry completes.
		observed.config, err = interfaceConfig{Enabled: true}, nil
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LAG interface configuration", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, interfaceState(state.LAGID.ValueInt64(), observed.config, state.PersistencePending.ValueBool()))...)
}

func (r *interfaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state interfaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := applyInterface(ctx, r.device, state.LAGID.ValueInt64(), interfaceConfig{Enabled: true}, false)
	if err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot reset LAG interface configuration", err.Error())
	}
}

func (r *interfaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(strings.TrimPrefix(req.ID, "lag "), 10, 64)
	if err != nil || id < 1 || req.ID != "lag "+strconv.FormatInt(id, 10) {
		resp.Diagnostics.AddError("Invalid LAG interface identity", "Use lag <id>, with a positive numeric identifier.")
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("lag_id"), id)...)
}

func (m interfaceModel) desired() interfaceConfig {
	return interfaceConfig{PortName: m.PortName.ValueString(), Enabled: m.Enabled.ValueBool()}
}

func interfaceState(id int64, observed interfaceConfig, pending bool) interfaceModel {
	return interfaceModel{ID: types.StringValue("lag " + strconv.FormatInt(id, 10)), LAGID: types.Int64Value(id), PortName: types.StringValue(observed.PortName), Enabled: types.BoolValue(observed.Enabled), PersistencePending: types.BoolValue(pending)}
}
