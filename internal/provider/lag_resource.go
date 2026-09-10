package provider

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type lagResource struct{ device *fastiron.Device }

type lagModel struct {
	ID                 types.String `tfsdk:"id"`
	LAGID              types.Int64  `tfsdk:"lag_id"`
	Name               types.String `tfsdk:"name"`
	Mode               types.String `tfsdk:"mode"`
	Members            types.Set    `tfsdk:"members"`
	PersistencePending types.Bool   `tfsdk:"persistence_pending"`
}

func (r *lagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lag"
}

func (r *lagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns a LAG's existence, name, mode, and Ethernet membership. Removing members or destroying the LAG disables detached ports. Interface settings and VLAN memberships remain independently managed. Import with lag <id>.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: lag <id>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"lag_id":              schema.Int64Attribute{Required: true, Description: "Positive native LAG identifier.", PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"name":                schema.StringAttribute{Required: true, Description: "Configured LAG name: 1 to 64 printable ASCII characters."},
		"mode":                schema.StringAttribute{Required: true, Description: "dynamic (LACP) or static. Changing mode replaces the LAG.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"members":             schema.SetAttribute{Required: true, ElementType: types.StringType, Description: "Complete set of canonical Ethernet member names. Use an empty set for an empty LAG."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
}

func (r *lagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m lagModel) known() bool {
	if m.LAGID.IsUnknown() || m.LAGID.IsNull() || m.Name.IsUnknown() || m.Name.IsNull() || m.Mode.IsUnknown() || m.Mode.IsNull() || m.Members.IsUnknown() || m.Members.IsNull() {
		return false
	}
	for _, member := range m.Members.Elements() {
		if member.IsUnknown() {
			return false
		}
	}
	return true
}

func (m lagModel) desired(ctx context.Context) (fastiron.LAG, diag.Diagnostics) {
	v := fastiron.LAG{ID: m.LAGID.ValueInt64(), Name: m.Name.ValueString(), Mode: m.Mode.ValueString()}
	diags := m.Members.ElementsAs(ctx, &v.Members, false)
	return v, diags
}

func (r *lagResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m lagModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !m.known() {
		return
	}

	v, diags := m.desired(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := fastiron.ValidateLAG(v); err != nil {
		resp.Diagnostics.AddError("Invalid LAG configuration", err.Error())
	}
}

func (r *lagResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	if r.device == nil {
		return
	}
	if _, err := r.device.LAGs(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot read LAG capability", err.Error())
	}
}

func (r *lagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m lagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	v, diags := m.desired(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyLAG(ctx, v)
	if observed != nil {
		state, diags := lagState(ctx, *observed, err != nil)
		resp.Diagnostics.Append(diags...)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot create LAG", err.Error())
	}
}

func (r *lagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m lagModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	v, diags := m.desired(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := r.device.ApplyLAG(ctx, v)
	if observed != nil {
		state, diags := lagState(ctx, *observed, err != nil)
		resp.Diagnostics.Append(diags...)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	} else if err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update LAG", err.Error())
	}
}

func (r *lagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	lag, err := r.device.LAG(ctx, state.LAGID.ValueInt64())
	if errors.Is(err, fastiron.ErrNotFound) {
		// Keep the identity until a failed delete's save can finish.
		if !state.PersistencePending.ValueBool() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LAG", err.Error())
		return
	}

	observed, diags := lagState(ctx, lag, state.PersistencePending.ValueBool())
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, observed)...)
}

func (r *lagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lagModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.device.DeleteLAG(ctx, state.LAGID.ValueInt64())
	if err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete LAG", err.Error())
	}
}

func (r *lagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(strings.TrimPrefix(req.ID, "lag "), 10, 64)
	if err != nil || id < 1 || req.ID != "lag "+strconv.FormatInt(id, 10) {
		resp.Diagnostics.AddError("Invalid LAG identity", "Use lag <id>, with a positive numeric identifier.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("lag_id"), id)...)
}

func lagState(ctx context.Context, v fastiron.LAG, pending bool) (lagModel, diag.Diagnostics) {
	members, diags := types.SetValueFrom(ctx, types.StringType, v.Members)
	return lagModel{ID: types.StringValue("lag " + strconv.FormatInt(v.ID, 10)), LAGID: types.Int64Value(v.ID), Name: types.StringValue(v.Name), Mode: types.StringValue(v.Mode), Members: members, PersistencePending: types.BoolValue(pending)}, diags
}
