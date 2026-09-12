package acl

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type macResource struct{ device *fastiron.Device }

func NewMACResource() *macResource { return &macResource{} }

type macModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Rules              types.List   `tfsdk:"rule"`
	PersistencePending types.Bool   `tfsdk:"persistence_pending"`
}

type macRuleModel struct {
	Action          types.String `tfsdk:"action"`
	Source          types.String `tfsdk:"source"`
	SourceMask      types.String `tfsdk:"source_mask"`
	Destination     types.String `tfsdk:"destination"`
	DestinationMask types.String `tfsdk:"destination_mask"`
	EtherType       types.Int64  `tfsdk:"ethertype"`
	Log             types.Bool   `tfsdk:"log"`
}

func macRuleType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"action": types.StringType, "source": types.StringType, "source_mask": types.StringType,
		"destination": types.StringType, "destination_mask": types.StringType,
		"ethertype": types.Int64Type, "log": types.BoolType,
	}}
}

func (r *macResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mac_access_list"
}

func (r *macResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	ruleAttributes := map[string]schema.Attribute{
		"action":           schema.StringAttribute{Required: true, Description: "permit or deny."},
		"source":           schema.StringAttribute{Optional: true, Computed: true, Description: "any or a lowercase colon-separated 48-bit MAC address. Defaults to any."},
		"source_mask":      schema.StringAttribute{Optional: true, Computed: true, Description: "MAC bit mask; one bits select matching address bits. Defaults to ff:ff:ff:ff:ff:ff for an address, or any for source any. Noncontiguous masks are supported."},
		"destination":      schema.StringAttribute{Optional: true, Computed: true, Description: "any or a lowercase colon-separated 48-bit MAC address. Defaults to any."},
		"destination_mask": schema.StringAttribute{Optional: true, Computed: true, Description: "MAC bit mask; one bits select matching address bits. Defaults to ff:ff:ff:ff:ff:ff for an address, or any for destination any. Noncontiguous masks are supported."},
		"ethertype":        schema.Int64Attribute{Optional: true, Description: "EtherType from 1536 through 65535, such as 2048 for IPv4 or 34525 for IPv6. Omit to match all EtherTypes. Firmware may reject particular values."},
		"log":              schema.BoolAttribute{Optional: true, Computed: true, Description: "Mark matching packets for syslog. Logging must also be enabled on the ACL binding. Defaults to false."},
	}
	resp.Schema = schema.Schema{
		Description: "Owns one MAC ACL and its complete ordered rule list. Bindings are separate; remove references before deletion.",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: mac access-list <name>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":                schema.StringAttribute{Required: true, Description: "MAC ACL name.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when reconciliation or saving must be retried after an error."},
		},
		Blocks: map[string]schema.Block{
			"rule": schema.ListNestedBlock{Description: "Rules are evaluated in declaration order. Omitting all rules manages an empty ACL.", NestedObject: schema.NestedBlockObject{Attributes: ruleAttributes}},
		},
	}
}

func (m macModel) desired(ctx context.Context) (macConfig, bool, diag.Diagnostics) {
	desired := macConfig{Name: m.Name.ValueString(), Rules: []macRule{}}
	if m.Name.IsUnknown() || m.Rules.IsUnknown() {
		return desired, false, nil
	}

	rules, diagnostics := configuredMACRules(ctx, m.Rules)
	if diagnostics.HasError() {
		return desired, false, diagnostics
	}
	for _, rule := range rules {
		for _, value := range []attr.Value{rule.Action, rule.Source, rule.SourceMask, rule.Destination, rule.DestinationMask, rule.EtherType, rule.Log} {
			if value.IsUnknown() {
				return desired, false, diagnostics
			}
		}

		source, err := configuredMACMatch(rule.Source.ValueString(), rule.SourceMask.ValueString())
		if err != nil {
			diagnostics.AddError("Invalid source MAC match", err.Error())
			return desired, false, diagnostics
		}
		destination, err := configuredMACMatch(rule.Destination.ValueString(), rule.DestinationMask.ValueString())
		if err != nil {
			diagnostics.AddError("Invalid destination MAC match", err.Error())
			return desired, false, diagnostics
		}

		desired.Rules = append(desired.Rules, macRule{Action: rule.Action.ValueString(), Source: source, Destination: destination, EtherType: optionalInt{Value: rule.EtherType.ValueInt64(), Present: !rule.EtherType.IsNull()}, Log: rule.Log.ValueBool()})
	}
	return desired, true, diagnostics
}

func configuredMACMatch(address, mask string) (macMatch, error) {
	match, err := parseMACMatch(address, mask)
	if err != nil {
		return match, err
	}
	if match.Mask == (macAddress{}) {
		if address != "any" || mask != "any" {
			return match, errors.New("use any instead of a zero MAC mask")
		}
		return match, nil
	}

	if match.Address.String() != address || match.Mask.String() != mask {
		return match, errors.New("MAC addresses and masks must use lowercase colon-separated notation")
	}
	return match, nil
}

func configuredMACRules(ctx context.Context, value types.List) ([]macRuleModel, diag.Diagnostics) {
	var rules []macRuleModel
	diagnostics := value.ElementsAs(ctx, &rules, false)
	if diagnostics.HasError() {
		return nil, diagnostics
	}

	for i := range rules {
		rule := &rules[i]
		rule.Source, rule.SourceMask = macDefaults(rule.Source, rule.SourceMask)
		rule.Destination, rule.DestinationMask = macDefaults(rule.Destination, rule.DestinationMask)
		if rule.Log.IsNull() {
			rule.Log = types.BoolValue(false)
		}
	}
	return rules, diagnostics
}

func macDefaults(address, mask types.String) (types.String, types.String) {
	if address.IsNull() {
		address = types.StringValue("any")
	}
	if !mask.IsNull() {
		return address, mask
	}
	if address.IsUnknown() {
		return address, types.StringUnknown()
	}
	if address.ValueString() == "any" {
		return address, types.StringValue("any")
	}
	return address, types.StringValue("ff:ff:ff:ff:ff:ff")
}

func macState(ctx context.Context, config macConfig, pending bool) (macModel, diag.Diagnostics) {
	rules := make([]macRuleModel, 0, len(config.Rules))
	for _, rule := range config.Rules {
		source, sourceMask := macMatchState(rule.Source)
		destination, destinationMask := macMatchState(rule.Destination)
		rules = append(rules, macRuleModel{Action: types.StringValue(rule.Action), Source: source, SourceMask: sourceMask, Destination: destination, DestinationMask: destinationMask, EtherType: optionalState(rule.EtherType), Log: types.BoolValue(rule.Log)})
	}

	value, diagnostics := types.ListValueFrom(ctx, macRuleType(), rules)
	return macModel{ID: types.StringValue("mac access-list " + config.Name), Name: types.StringValue(config.Name), Rules: value, PersistencePending: types.BoolValue(pending)}, diagnostics
}

func macMatchState(match macMatch) (types.String, types.String) {
	if match.Mask == (macAddress{}) {
		return types.StringValue("any"), types.StringValue("any")
	}
	return types.StringValue(match.Address.String()), types.StringValue(match.Mask.String())
}

func (r *macResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (r *macResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model macModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, known, diagnostics := model.desired(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if !known || resp.Diagnostics.HasError() {
		return
	}
	if err := validateMAC(desired); err != nil {
		resp.Diagnostics.AddError("Invalid MAC ACL", err.Error())
	}
}

func (r *macResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var model macModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Derive defaults from configured rules so drift cannot substitute fields
	// from another list position.
	if !model.Rules.IsUnknown() {
		rules, diagnostics := configuredMACRules(ctx, model.Rules)
		resp.Diagnostics.Append(diagnostics...)
		if resp.Diagnostics.HasError() {
			return
		}
		planned, diagnostics := types.ListValueFrom(ctx, macRuleType(), rules)
		resp.Diagnostics.Append(diagnostics...)
		if resp.Diagnostics.HasError() {
			return
		}
		model.Rules = planned
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("rule"), planned)...)
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	if r.device == nil || resp.Diagnostics.HasError() {
		return
	}
	if !r.device.RESTCONFEnabled() {
		resp.Diagnostics.AddError("MAC ACL configuration is not supported by the configured transport", "RESTCONF is required.")
		return
	}

	desired, known, diagnostics := model.desired(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if !known || resp.Diagnostics.HasError() {
		return
	}
	if err := validateMAC(desired); err != nil {
		resp.Diagnostics.AddError("Invalid MAC ACL", err.Error())
		return
	}
	if _, err := readMAC(ctx, r.device, desired.Name); err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot read MAC ACL", err.Error())
	}
}

func (r *macResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model macModel
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
	observed, err := applyMAC(ctx, r.device, desired)
	if observed != nil {
		state, diagnostics := macState(ctx, *observed, err != nil)
		resp.Diagnostics.Append(diagnostics...)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply MAC ACL", err.Error())
	}
}

func (r *macResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model macModel
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
	observed, err := applyMAC(ctx, r.device, desired)
	if observed != nil {
		state, diagnostics := macState(ctx, *observed, err != nil)
		resp.Diagnostics.Append(diagnostics...)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		// An update may remove the parent before failing. Preserve its unsaved
		// absence through refresh so a subsequent destroy can persist it.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot update MAC ACL", err.Error())
	}
}

func (r *macResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model macModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := readMAC(ctx, r.device, model.Name.ValueString())
	if errors.Is(err, fastiron.ErrNotFound) {
		// An unsaved deletion must retain state so a later destroy can save the
		// verified absence instead of forgetting the pending operation.
		if !model.PersistencePending.ValueBool() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read MAC ACL", err.Error())
		return
	}
	state, diagnostics := macState(ctx, *observed, model.PersistencePending.ValueBool())
	resp.Diagnostics.Append(diagnostics...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *macResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model macModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteMAC(ctx, r.device, model.Name.ValueString()); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete MAC ACL", err.Error())
	}
}

func (r *macResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	prefix := "mac access-list "
	if !strings.HasPrefix(req.ID, prefix) {
		resp.Diagnostics.AddError("Invalid MAC ACL identity", "Use "+prefix+"<name>.")
		return
	}
	name := strings.TrimPrefix(req.ID, prefix)
	if err := validateMAC(macConfig{Name: name}); err != nil {
		resp.Diagnostics.AddError("Invalid MAC ACL identity", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}
