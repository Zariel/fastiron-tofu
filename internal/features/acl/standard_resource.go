package acl

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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

type StandardResource struct{ device *fastiron.Device }

type standardModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Rules              types.Set    `tfsdk:"rule"`
	PersistencePending types.Bool   `tfsdk:"persistence_pending"`
}

type standardRuleModel struct {
	Sequence types.Int64  `tfsdk:"sequence"`
	Action   types.String `tfsdk:"action"`
	Source   types.String `tfsdk:"source"`
}

var standardRuleType = types.ObjectType{AttrTypes: map[string]attr.Type{"sequence": types.Int64Type, "action": types.StringType, "source": types.StringType}}

func (r *StandardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_access_list_standard"
}

func (r *StandardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one numbered IPv4 standard ACL and its ordered source-address rules. Bindings are separate; delete requires removing references first.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: ip access-list standard <name>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":                schema.StringAttribute{Required: true, Description: "Canonical ACL number from 1 through 99. Named standard ACLs are not supported by RESTCONF.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when reconciliation or saving must be retried after an error."},
	}, Blocks: map[string]schema.Block{
		"rule": schema.SetNestedBlock{Description: "Rules are evaluated by sequence. Omitting all rules manages an empty ACL.", NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
			"sequence": schema.Int64Attribute{Required: true, Description: "Distinct sequence number from 1 through 65000."},
			"action":   schema.StringAttribute{Required: true, Description: "permit or deny."},
			"source":   schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("any"), Description: "any or a canonical IPv4 prefix, including /32 for one host. Defaults to any."},
		}}},
	}}
}

func (r *StandardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m standardModel) desired(ctx context.Context) (standardConfig, bool, diag.Diagnostics) {
	p := standardConfig{Name: m.Name.ValueString(), Rules: map[int64]standardRule{}}
	if m.Name.IsUnknown() || m.Rules.IsUnknown() {
		return p, false, nil
	}
	var rules []standardRuleModel
	diagnostics := m.Rules.ElementsAs(ctx, &rules, false)
	if diagnostics.HasError() {
		return p, false, diagnostics
	}
	for _, rule := range rules {
		if rule.Sequence.IsUnknown() || rule.Action.IsUnknown() || rule.Source.IsUnknown() {
			return p, false, diagnostics
		}
		sequence := rule.Sequence.ValueInt64()
		if _, exists := p.Rules[sequence]; exists {
			diagnostics.AddError("Duplicate ACL sequence", "Each rule must have a distinct sequence number.")
			return p, false, diagnostics
		}
		source := rule.Source.ValueString()
		if rule.Source.IsNull() {
			source = "any"
		}
		p.Rules[sequence] = standardRule{Sequence: sequence, Action: rule.Action.ValueString(), Source: source}
	}
	return p, true, diagnostics
}

func (r *StandardResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model standardModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, known, diagnostics := model.desired(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if !known || resp.Diagnostics.HasError() {
		return
	}
	if err := validateStandard(desired); err != nil {
		resp.Diagnostics.AddError("Invalid standard ACL", err.Error())
	}
}

func (r *StandardResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	if !r.device.RESTCONFEnabled() {
		resp.Diagnostics.AddError("Standard ACL configuration is not supported by the configured transport", "RESTCONF is required.")
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var model standardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, known, diagnostics := model.desired(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if !known || resp.Diagnostics.HasError() {
		return
	}
	if err := validateStandard(desired); err != nil {
		resp.Diagnostics.AddError("Invalid standard ACL", err.Error())
		return
	}
	if _, err := readStandard(ctx, r.device, desired.Name); err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot read standard ACL", err.Error())
	}
}

func (r *StandardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model standardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, known, diagnostics := model.desired(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !known {
		resp.Diagnostics.AddError("Unknown ACL configuration", "All ACL rule values must be known before application.")
		return
	}
	observed, err := applyStandard(ctx, r.device, desired)
	if observed != nil {
		state, diagnostics := standardState(ctx, *observed, err != nil)
		resp.Diagnostics.Append(diagnostics...)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply standard ACL", err.Error())
	}
}

func (r *StandardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model standardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, known, diagnostics := model.desired(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !known {
		resp.Diagnostics.AddError("Unknown ACL configuration", "All ACL rule values must be known before application.")
		return
	}
	observed, err := applyStandard(ctx, r.device, desired)
	if observed != nil {
		state, diagnostics := standardState(ctx, *observed, err != nil)
		resp.Diagnostics.Append(diagnostics...)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		// An update may remove the parent before failing. Preserve its unsaved
		// absence through refresh so a subsequent destroy can persist it.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot update standard ACL", err.Error())
	}
}

func (r *StandardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model standardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := readStandard(ctx, r.device, model.Name.ValueString())
	if errors.Is(err, fastiron.ErrNotFound) {
		// An unsaved deletion must retain state so a later destroy can save the
		// verified absence instead of forgetting the pending operation.
		if !model.PersistencePending.ValueBool() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read standard ACL", err.Error())
		return
	}
	state, diagnostics := standardState(ctx, *observed, model.PersistencePending.ValueBool())
	resp.Diagnostics.Append(diagnostics...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *StandardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model standardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteStandard(ctx, r.device, model.Name.ValueString()); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete standard ACL", err.Error())
	}
}

func (r *StandardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	const prefix = "ip access-list standard "
	if !strings.HasPrefix(req.ID, prefix) {
		resp.Diagnostics.AddError("Invalid standard ACL identity", "Use ip access-list standard <number>.")
		return
	}
	name := strings.TrimPrefix(req.ID, prefix)
	if err := validateStandard(standardConfig{Name: name}); err != nil {
		resp.Diagnostics.AddError("Invalid standard ACL identity", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

func standardState(ctx context.Context, p standardConfig, pending bool) (standardModel, diag.Diagnostics) {
	rules := []standardRuleModel{}
	for _, sequence := range slices.Sorted(maps.Keys(p.Rules)) {
		rule := p.Rules[sequence]
		rules = append(rules, standardRuleModel{Sequence: types.Int64Value(sequence), Action: types.StringValue(rule.Action), Source: types.StringValue(rule.Source)})
	}
	value, diagnostics := types.SetValueFrom(ctx, standardRuleType, rules)
	return standardModel{ID: types.StringValue("ip access-list standard " + p.Name), Name: types.StringValue(p.Name), Rules: value, PersistencePending: types.BoolValue(pending)}, diagnostics
}
