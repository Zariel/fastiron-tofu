package acl

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
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

type ExtendedResource struct{ device *fastiron.Device }

type extendedModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Rules              types.Set    `tfsdk:"rule"`
	PersistencePending types.Bool   `tfsdk:"persistence_pending"`
}

type extendedRuleModel struct {
	Sequence        types.Int64  `tfsdk:"sequence"`
	Action          types.String `tfsdk:"action"`
	Source          types.String `tfsdk:"source"`
	Destination     types.String `tfsdk:"destination"`
	Protocol        types.Int64  `tfsdk:"protocol"`
	SourcePort      types.String `tfsdk:"source_port"`
	DestinationPort types.String `tfsdk:"destination_port"`
	DSCP            types.Int64  `tfsdk:"dscp"`
	DSCPMark        types.Int64  `tfsdk:"dscp_marking"`
	Priority        types.Int64  `tfsdk:"internal_priority_marking"`
}

var extendedRuleType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"sequence": types.Int64Type, "action": types.StringType, "source": types.StringType, "destination": types.StringType,
	"protocol": types.Int64Type, "source_port": types.StringType, "destination_port": types.StringType,
	"dscp": types.Int64Type, "dscp_marking": types.Int64Type, "internal_priority_marking": types.Int64Type,
}}

func (r *ExtendedResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_access_list_extended"
}

func (r *ExtendedResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one IPv4 extended ACL and its ordered packet rules. Bindings are separate; delete requires removing references first.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, Description: "Canonical identity: ip access-list extended <name>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":                schema.StringAttribute{Required: true, Description: "ACL name or canonical extended ACL number from 100 through 199.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when reconciliation or saving must be retried after an error."},
	}, Blocks: map[string]schema.Block{
		"rule": schema.SetNestedBlock{Description: "Rules are evaluated by sequence. Omitting all rules manages an empty ACL.", NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
			"sequence":                  schema.Int64Attribute{Required: true, Description: "Distinct sequence number from 1 through 65000."},
			"action":                    schema.StringAttribute{Required: true, Description: "permit or deny."},
			"source":                    schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("any"), Description: "any or a canonical IPv4 prefix, including /32 for one host. Defaults to any."},
			"destination":               schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("any"), Description: "any or a canonical IPv4 prefix, including /32 for one host. Defaults to any."},
			"protocol":                  schema.Int64Attribute{Optional: true, Description: "IP protocol number from 1 through 254, such as 6 for TCP or 17 for UDP. Omit to match all protocols."},
			"source_port":               schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("any"), Description: "any, a canonical decimal port, or an inclusive lower..upper range. Numeric matches require TCP or UDP."},
			"destination_port":          schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("any"), Description: "any, a canonical decimal port, or an inclusive lower..upper range. Numeric matches require TCP or UDP."},
			"dscp":                      schema.Int64Attribute{Optional: true, Description: "DSCP match from 0 through 63. Omit to match any DSCP; zero is an explicit match."},
			"dscp_marking":              schema.Int64Attribute{Optional: true, Description: "DSCP marking from 0 through 63. Omit to leave DSCP unchanged."},
			"internal_priority_marking": schema.Int64Attribute{Optional: true, Description: "Internal priority marking from 0 through 7. Omit to leave priority unchanged."},
		}}},
	}}
}

func (r *ExtendedResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m extendedModel) desired(ctx context.Context) (ipConfig, bool, diag.Diagnostics) {
	p := ipConfig{Family: ipv4ACL, Name: m.Name.ValueString(), Rules: map[int64]ipRule{}}
	if m.Name.IsUnknown() || m.Rules.IsUnknown() {
		return p, false, nil
	}
	var rules []extendedRuleModel
	diagnostics := m.Rules.ElementsAs(ctx, &rules, false)
	if diagnostics.HasError() {
		return p, false, diagnostics
	}
	for _, rule := range rules {
		for _, value := range []types.Int64{rule.Sequence, rule.Protocol, rule.DSCP, rule.DSCPMark, rule.Priority} {
			if value.IsUnknown() {
				return p, false, diagnostics
			}
		}
		for _, value := range []types.String{rule.Action, rule.Source, rule.Destination, rule.SourcePort, rule.DestinationPort} {
			if value.IsUnknown() {
				return p, false, diagnostics
			}
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
		destination := rule.Destination.ValueString()
		if rule.Destination.IsNull() {
			destination = "any"
		}
		sourcePort, err := configuredPort(rule.SourcePort)
		if err != nil {
			diagnostics.AddError("Invalid source port", err.Error())
			return p, false, diagnostics
		}
		destinationPort, err := configuredPort(rule.DestinationPort)
		if err != nil {
			diagnostics.AddError("Invalid destination port", err.Error())
			return p, false, diagnostics
		}
		p.Rules[sequence] = ipRule{
			Sequence: sequence, Action: rule.Action.ValueString(), Source: source, Destination: destination,
			Protocol:   optionalInt{rule.Protocol.ValueInt64(), !rule.Protocol.IsNull()},
			SourcePort: sourcePort, DestinationPort: destinationPort,
			DSCP:     optionalInt{rule.DSCP.ValueInt64(), !rule.DSCP.IsNull()},
			DSCPMark: optionalInt{rule.DSCPMark.ValueInt64(), !rule.DSCPMark.IsNull()},
			Priority: optionalInt{rule.Priority.ValueInt64(), !rule.Priority.IsNull()},
		}
	}
	return p, true, diagnostics
}

func (r *ExtendedResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model extendedModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, known, diagnostics := model.desired(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if !known || resp.Diagnostics.HasError() {
		return
	}
	if err := validateIP(desired); err != nil {
		resp.Diagnostics.AddError("Invalid extended ACL", err.Error())
	}
}

func (r *ExtendedResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.device == nil {
		return
	}
	if !r.device.RESTCONFEnabled() {
		resp.Diagnostics.AddError("Extended ACL configuration is not supported by the configured transport", "RESTCONF is required.")
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var model extendedModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, known, diagnostics := model.desired(ctx)
	resp.Diagnostics.Append(diagnostics...)
	if !known || resp.Diagnostics.HasError() {
		return
	}
	if err := validateIP(desired); err != nil {
		resp.Diagnostics.AddError("Invalid extended ACL", err.Error())
		return
	}
	if _, err := readIP(ctx, r.device, ipv4ACL, desired.Name); err != nil && !errors.Is(err, fastiron.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot read extended ACL", err.Error())
	}
}

func (r *ExtendedResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model extendedModel
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
	observed, err := applyIP(ctx, r.device, desired)
	if observed != nil {
		state, diagnostics := extendedState(ctx, *observed, err != nil)
		resp.Diagnostics.Append(diagnostics...)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot apply extended ACL", err.Error())
	}
}

func (r *ExtendedResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model extendedModel
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
	observed, err := applyIP(ctx, r.device, desired)
	if observed != nil {
		state, diagnostics := extendedState(ctx, *observed, err != nil)
		resp.Diagnostics.Append(diagnostics...)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot update extended ACL", err.Error())
	}
}

func (r *ExtendedResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model extendedModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := readIP(ctx, r.device, ipv4ACL, model.Name.ValueString())
	if errors.Is(err, fastiron.ErrNotFound) {
		// A failed save after deletion must retain state so a destroy retry can save
		// the verified absence instead of silently forgetting the pending operation.
		if !model.PersistencePending.ValueBool() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read extended ACL", err.Error())
		return
	}
	state, diagnostics := extendedState(ctx, *observed, model.PersistencePending.ValueBool())
	resp.Diagnostics.Append(diagnostics...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ExtendedResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model extendedModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := deleteIP(ctx, r.device, ipv4ACL, model.Name.ValueString()); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete extended ACL", err.Error())
	}
}

func (r *ExtendedResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	const prefix = "ip access-list extended "
	if !strings.HasPrefix(req.ID, prefix) {
		resp.Diagnostics.AddError("Invalid extended ACL identity", "Use ip access-list extended <name>.")
		return
	}
	name := strings.TrimPrefix(req.ID, prefix)
	if err := validateIP(ipConfig{Family: ipv4ACL, Name: name}); err != nil {
		resp.Diagnostics.AddError("Invalid extended ACL identity", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

func extendedState(ctx context.Context, p ipConfig, pending bool) (extendedModel, diag.Diagnostics) {
	rules := []extendedRuleModel{}
	for _, sequence := range slices.Sorted(maps.Keys(p.Rules)) {
		rule := p.Rules[sequence]
		rules = append(rules, extendedRuleModel{
			Sequence: types.Int64Value(sequence), Action: types.StringValue(rule.Action), Source: types.StringValue(rule.Source), Destination: types.StringValue(rule.Destination),
			Protocol: optionalState(rule.Protocol), SourcePort: types.StringValue(rule.SourcePort.String()), DestinationPort: types.StringValue(rule.DestinationPort.String()),
			DSCP: optionalState(rule.DSCP), DSCPMark: optionalState(rule.DSCPMark), Priority: optionalState(rule.Priority),
		})
	}
	value, diagnostics := types.SetValueFrom(ctx, extendedRuleType, rules)
	return extendedModel{ID: types.StringValue("ip access-list extended " + p.Name), Name: types.StringValue(p.Name), Rules: value, PersistencePending: types.BoolValue(pending)}, diagnostics
}

func configuredPort(value types.String) (portMatch, error) {
	if value.IsNull() || value.ValueString() == "any" {
		return portMatch{}, nil
	}
	text := value.ValueString()
	firstText, lastText, hasRange := strings.Cut(text, "..")
	first, err := strconv.ParseInt(firstText, 10, 64)
	if err != nil {
		return portMatch{}, fmt.Errorf("port must be any, a decimal number, or lower..upper")
	}
	last := first
	if hasRange {
		last, err = strconv.ParseInt(lastText, 10, 64)
		if err != nil {
			return portMatch{}, fmt.Errorf("range endpoint must be a decimal port number")
		}
	}
	port := portMatch{first, last, true}
	if port.String() != text {
		return portMatch{}, fmt.Errorf("port must use canonical decimal notation; use a single number for equal range endpoints")
	}
	return port, nil
}

func optionalState(value optionalInt) types.Int64 {
	if !value.Present {
		return types.Int64Null()
	}
	return types.Int64Value(value.Value)
}
