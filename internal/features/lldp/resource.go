package lldp

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	tfresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type resource struct {
	device       *fastiron.Device
	perInterface bool
}

func (r *resource) Metadata(_ context.Context, req tfresource.MetadataRequest, resp *tfresource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lldp"
	if r.perInterface {
		resp.TypeName += "_interface"
	}
}

func (r *resource) Schema(_ context.Context, _ tfresource.SchemaRequest, resp *tfresource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns LLDP enable state. Omission and destroy restore enabled=true. Global and per-interface settings are independently owned.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"enabled":             schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), Description: "Enable LLDP in this scope. Defaults to true."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when a failed operation still requires reconciliation or persistence."},
	}}
	if r.perInterface {
		resp.Schema.Attributes["interface"] = schema.StringAttribute{Required: true, Description: "Canonical Ethernet interface name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}
	}
}

func (r *resource) Configure(_ context.Context, req tfresource.ConfigureRequest, resp *tfresource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *resource) ValidateConfig(ctx context.Context, req tfresource.ValidateConfigRequest, resp *tfresource.ValidateConfigResponse) {
	if !r.perInterface {
		return
	}
	var name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("interface"), &name)...)
	if resp.Diagnostics.HasError() || name.IsUnknown() || name.IsNull() {
		return
	}
	if err := validateInterface(name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Invalid LLDP interface", err.Error())
	}
}

func (r *resource) ModifyPlan(ctx context.Context, req tfresource.ModifyPlanRequest, resp *tfresource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	if r.device == nil {
		return
	}
	var name types.String
	if r.perInterface {
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("interface"), &name)...)
		if name.IsUnknown() || name.IsNull() {
			return
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := readEnabled(ctx, r.device, name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Cannot read LLDP capability", err.Error())
	}
}

func (r *resource) Create(ctx context.Context, req tfresource.CreateRequest, resp *tfresource.CreateResponse) {
	var enabled types.Bool
	var name types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("enabled"), &enabled)...)
	if r.perInterface {
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("interface"), &name)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyEnabled(ctx, r.device, name.ValueString(), enabled.ValueBool())
	if observed != nil {
		resp.State.Raw = req.Plan.Raw
		id := "lldp"
		if r.perInterface {
			id += "|" + name.ValueString()
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), *observed)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), err != nil)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply LLDP", err.Error())
	}
}

func (r *resource) Update(ctx context.Context, req tfresource.UpdateRequest, resp *tfresource.UpdateResponse) {
	var enabled types.Bool
	var name types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("enabled"), &enabled)...)
	if r.perInterface {
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("interface"), &name)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := applyEnabled(ctx, r.device, name.ValueString(), enabled.ValueBool())
	if observed != nil {
		resp.State.Raw = req.Plan.Raw
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), *observed)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), err != nil)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update LLDP", err.Error())
	}
}

func (r *resource) Read(ctx context.Context, req tfresource.ReadRequest, resp *tfresource.ReadResponse) {
	var name types.String
	var pending types.Bool
	if r.perInterface {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("interface"), &name)...)
	}
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("persistence_pending"), &pending)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enabled, err := readEnabled(ctx, r.device, name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LLDP", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), enabled)...)
	if pending.IsNull() {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	}
}

func (r *resource) Delete(ctx context.Context, req tfresource.DeleteRequest, resp *tfresource.DeleteResponse) {
	var name types.String
	if r.perInterface {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("interface"), &name)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := applyEnabled(ctx, r.device, name.ValueString(), true); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot reset LLDP", err.Error())
	}
}

func (r *resource) ImportState(ctx context.Context, req tfresource.ImportStateRequest, resp *tfresource.ImportStateResponse) {
	if r.perInterface {
		name := strings.TrimPrefix(req.ID, "lldp|")
		if req.ID != "lldp|"+name || validateInterface(name) != nil {
			resp.Diagnostics.AddError("Invalid LLDP identity", "Use lldp|ethernet <stack>/<slot>/<port>.")
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("interface"), name)...)
	} else if req.ID != "lldp" {
		resp.Diagnostics.AddError("Invalid LLDP identity", "Use lldp for global LLDP.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func NewGlobalResource() *resource    { return &resource{} }
func NewInterfaceResource() *resource { return &resource{perInterface: true} }
