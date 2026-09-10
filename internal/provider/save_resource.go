package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	saveResource struct{ device *fastiron.Device }
	saveModel    struct {
		ID       types.String `tfsdk:"id"`
		Revision types.String `tfsdk:"revision"`
	}
)

func (r *saveResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_configuration_save"
}

func (r *saveResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Saves running configuration on create and revision changes. Use depends_on to order after the resources being persisted. Destroy has no remote effect. This action resource does not represent an importable remote object.", Attributes: map[string]schema.Attribute{
		"id":       schema.StringAttribute{Computed: true, Description: "Local save action identity.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"revision": schema.StringAttribute{Required: true, Description: "Changing this value requests another save."},
	}}
}

func (r *saveResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *saveResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan saveModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.device.Save(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot save configuration", err.Error())
		return
	}
	plan.ID = types.StringValue("configuration-save")
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *saveResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan saveModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.device.Save(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot save configuration", err.Error())
		return
	}
	plan.ID = types.StringValue("configuration-save")
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}
func (r *saveResource) Read(context.Context, resource.ReadRequest, *resource.ReadResponse)       {}
func (r *saveResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {}
