package acl

import (
	"context"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type accessGroupResource struct {
	device *fastiron.Device
	family string
}

func NewIPAccessGroupResource() *accessGroupResource   { return &accessGroupResource{family: "ip"} }
func NewIPv6AccessGroupResource() *accessGroupResource { return &accessGroupResource{family: "ipv6"} }

func NewMACAccessGroupResource() *accessGroupResource { return &accessGroupResource{family: "mac"} }

type accessGroupModel struct {
	ID                 types.String `tfsdk:"id"`
	Interface          types.String `tfsdk:"interface"`
	Direction          types.String `tfsdk:"direction"`
	ACL                types.String `tfsdk:"acl"`
	PersistencePending types.Bool   `tfsdk:"persistence_pending"`
}

func (m accessGroupModel) key(family string) accessGroupKey {
	return accessGroupKey{m.Interface.ValueString(), family, m.Direction.ValueString()}
}

func (m accessGroupModel) unknown() bool {
	return m.Interface.IsUnknown() || m.Direction.IsUnknown() || m.ACL.IsUnknown()
}

func (r *accessGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.family + "_access_group"
}

func (r *accessGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one interface/family/direction ACL binding. ACL rules and other binding slots remain separately owned.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: <interface> <direction>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"interface":           schema.StringAttribute{Required: true, Description: "Canonical Ethernet or LAG interface, such as ethernet 1/1/9 or lag 1.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"direction":           schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("in"), Description: "in or out; defaults to in. MAC ACLs support in only.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"acl":                 schema.StringAttribute{Required: true, Description: "Name of an existing ACL of the resource's family. Changes replace the active binding in place."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when binding reconciliation, stale REST entry cleanup or saving needs a retry."},
	}}
}

func (r *accessGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *accessGroupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model accessGroupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || model.unknown() {
		return
	}
	if model.Direction.IsNull() {
		model.Direction = types.StringValue("in")
	}
	if err := model.key(r.family).validate(); err != nil {
		resp.Diagnostics.AddError("Invalid ACL binding", err.Error())
	}
	if err := validateACLName(model.ACL.ValueString()); err != nil {
		resp.Diagnostics.AddError("Invalid ACL name", err.Error())
	}
}

func (r *accessGroupResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	if !r.device.RESTCONFEnabled() {
		resp.Diagnostics.AddError("ACL binding transport is not supported", "RESTCONF is required.")
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var model accessGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() || model.unknown() {
		return
	}
	// A referenced ACL or LAG may be created earlier in the same apply. Parent
	// existence is checked under the write lock, after dependencies have completed.
	if _, err := readAccessGroup(ctx, r.device, model.key(r.family)); err != nil {
		resp.Diagnostics.AddError("Cannot inspect ACL binding", err.Error())
	}
}

func (r *accessGroupResource) apply(ctx context.Context, model accessGroupModel) (*accessGroupModel, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	if model.unknown() {
		diagnostics.AddError("Unknown ACL binding", "All binding values must be known before application.")
		return nil, diagnostics
	}
	k := model.key(r.family)
	view, err := applyAccessGroup(ctx, r.device, k, model.ACL.ValueString())
	if err != nil {
		diagnostics.AddError("Cannot apply ACL binding", err.Error())
	}
	if view == nil {
		return nil, diagnostics
	}
	model.ID = types.StringValue(k.Interface + " " + k.Direction)
	model.PersistencePending = types.BoolValue(err != nil)
	if view.ACL != "" {
		model.ACL = types.StringValue(view.ACL)
	}
	return &model, diagnostics
}

func (r *accessGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model accessGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state, diagnostics := r.apply(ctx, model)
	resp.Diagnostics.Append(diagnostics...)
	if state != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
}

func (r *accessGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model accessGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state, diagnostics := r.apply(ctx, model)
	resp.Diagnostics.Append(diagnostics...)
	if state != nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
}

func (r *accessGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model accessGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	k := model.key(r.family)
	view, err := readAccessGroup(ctx, r.device, k)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read ACL binding", err.Error())
		return
	}
	if view.ACL == "" && model.ACL.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}
	if view.ACL == "" && len(view.RESTACLs) == 0 && !model.PersistencePending.ValueBool() {
		resp.State.RemoveResource(ctx)
		return
	}
	expected := []string{}
	if view.ACL != "" {
		expected = []string{view.ACL}
		model.ACL = types.StringValue(view.ACL)
	}
	// Preserve recovery state for a failed save or stale REST-only binding so
	// refresh cannot bypass the cleanup that a subsequent update/destroy must do.
	pending := model.PersistencePending.ValueBool() || !slices.Equal(view.RESTACLs, expected)
	model.PersistencePending = types.BoolValue(pending)
	model.ID = types.StringValue(k.Interface + " " + k.Direction)
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *accessGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model accessGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := applyAccessGroup(ctx, r.device, model.key(r.family), ""); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete ACL binding", err.Error())
	}
}

func (r *accessGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	fields := strings.Fields(req.ID)
	if len(fields) != 3 || strings.Join(fields, " ") != req.ID {
		resp.Diagnostics.AddError("Invalid ACL binding identity", "Use <interface> <direction>, such as ethernet 1/1/9 in or lag 1 out.")
		return
	}
	k := accessGroupKey{fields[0] + " " + fields[1], r.family, fields[2]}
	if err := k.validate(); err != nil {
		resp.Diagnostics.AddError("Invalid ACL binding identity", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), k.Interface)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("direction"), k.Direction)...)
}
