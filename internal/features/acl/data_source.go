package acl

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type detailDataSource struct{ device *fastiron.Device }

type detailRule struct {
	ipRuleFields
	SourceMask      types.String `tfsdk:"source_mask"`
	DestinationMask types.String `tfsdk:"destination_mask"`
	EtherType       types.Int64  `tfsdk:"ethertype"`
	Log             types.Bool   `tfsdk:"log"`
}

func NewDataSource() *detailDataSource { return &detailDataSource{} }

func (d *detailDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_acl"
}

func (d *detailDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	fields := map[string]schema.Attribute{
		"sequence":                  schema.Int64Attribute{Computed: true, Description: "Native IP rule sequence. Null for MAC rules, whose list position determines order."},
		"action":                    schema.StringAttribute{Computed: true, Description: "permit or deny."},
		"source":                    schema.StringAttribute{Computed: true, Description: "Source IP prefix, MAC address or any."},
		"destination":               schema.StringAttribute{Computed: true, Description: "Destination IP prefix, MAC address or any. Null for standard IPv4 rules."},
		"source_mask":               schema.StringAttribute{Computed: true, Description: "MAC source bit mask or any; null for IP rules."},
		"destination_mask":          schema.StringAttribute{Computed: true, Description: "MAC destination bit mask or any; null for IP rules."},
		"ethertype":                 schema.Int64Attribute{Computed: true, Description: "MAC EtherType match; null when omitted or for IP rules."},
		"protocol":                  schema.Int64Attribute{Computed: true, Description: "IP protocol number; null when omitted or inapplicable."},
		"source_port":               schema.StringAttribute{Computed: true, Description: "IP source port, inclusive range or any; null for standard IPv4 and MAC rules."},
		"destination_port":          schema.StringAttribute{Computed: true, Description: "IP destination port, inclusive range or any; null for standard IPv4 and MAC rules."},
		"dscp":                      schema.Int64Attribute{Computed: true, Description: "IP DSCP match; null when omitted or inapplicable."},
		"dscp_marking":              schema.Int64Attribute{Computed: true, Description: "IP DSCP marking; null when omitted or inapplicable."},
		"internal_priority_marking": schema.Int64Attribute{Computed: true, Description: "IP internal priority marking; null when omitted or inapplicable."},
		"log":                       schema.BoolAttribute{Computed: true, Description: "IPv6 or MAC syslog action; null for IPv4, whose logging options are outside the supported rule model."},
	}
	resp.Schema = schema.Schema{Description: "Reads one ACL's supported native rules without taking ownership. Missing ACLs and unsupported rule options produce errors; existing empty ACLs return an empty list.", Attributes: map[string]schema.Attribute{
		"kind":  schema.StringAttribute{Required: true, Description: "ipv4_standard, ipv4_extended, ipv6 or mac; matches fastiron_acls inventory kinds."},
		"name":  schema.StringAttribute{Required: true, Description: "Configured ACL name."},
		"id":    schema.StringAttribute{Computed: true, Description: "Native ACL identity used for resource imports."},
		"rules": schema.ListNestedAttribute{Computed: true, Description: "Rules in native evaluation order. Fields that do not apply to this ACL kind are null.", NestedObject: schema.NestedAttributeObject{Attributes: fields}},
	}}
}

func (d *detailDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *detailDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var kind, name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("kind"), &kind)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if kind.IsUnknown() || name.IsUnknown() {
		resp.Diagnostics.AddError("Unknown ACL identity", "ACL kind and name must be known before reading.")
		return
	}
	if err := validateACLName(name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Invalid ACL name", err.Error())
		return
	}
	id, rules, err := d.readRules(ctx, kind.ValueString(), name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read ACL", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, struct {
		Kind  types.String `tfsdk:"kind"`
		Name  types.String `tfsdk:"name"`
		ID    string       `tfsdk:"id"`
		Rules []detailRule `tfsdk:"rules"`
	}{Kind: kind, Name: name, ID: id, Rules: rules})...)
}

func (d *detailDataSource) readRules(ctx context.Context, kind, name string) (string, []detailRule, error) {
	rules := []detailRule{}
	switch kind {
	case "ipv4_standard":
		current, err := readStandard(ctx, d.device, name)
		if err != nil {
			return "", nil, err
		}
		for _, sequence := range slices.Sorted(maps.Keys(current.Rules)) {
			rule := current.Rules[sequence]
			rules = append(rules, detailRule{ipRuleFields: ipRuleFields{Sequence: types.Int64Value(sequence), Action: types.StringValue(rule.Action), Source: types.StringValue(rule.Source)}})
		}
		return "ip access-list standard " + name, rules, nil
	case "ipv4_extended", "ipv6":
		family := ipv4ACL
		if kind == "ipv6" {
			family = ipv6ACL
		}
		current, err := readIP(ctx, d.device, family, name)
		if err != nil {
			return "", nil, err
		}
		for _, sequence := range slices.Sorted(maps.Keys(current.Rules)) {
			rule := current.Rules[sequence]
			observed := detailRule{ipRuleFields: ipRuleFields{
				Sequence: types.Int64Value(sequence), Action: types.StringValue(rule.Action), Source: types.StringValue(rule.Source), Destination: types.StringValue(rule.Destination),
				Protocol: optionalState(rule.Protocol), SourcePort: types.StringValue(rule.SourcePort.String()), DestinationPort: types.StringValue(rule.DestinationPort.String()),
				DSCP: optionalState(rule.DSCP), DSCPMark: optionalState(rule.DSCPMark), Priority: optionalState(rule.Priority),
			}}
			if family == ipv6ACL {
				observed.Log = types.BoolValue(rule.Log)
			}
			rules = append(rules, observed)
		}
		return family.header(name), rules, nil
	case "mac":
		current, err := readMAC(ctx, d.device, name)
		if err != nil {
			return "", nil, err
		}
		for _, rule := range current.Rules {
			source, sourceMask := macMatchState(rule.Source)
			destination, destinationMask := macMatchState(rule.Destination)
			rules = append(rules, detailRule{
				ipRuleFields: ipRuleFields{Action: types.StringValue(rule.Action), Source: source, Destination: destination},
				SourceMask:   sourceMask, DestinationMask: destinationMask, EtherType: optionalState(rule.EtherType), Log: types.BoolValue(rule.Log),
			})
		}
		return "mac access-list " + name, rules, nil
	default:
		return "", nil, errors.New("ACL kind must be ipv4_standard, ipv4_extended, ipv6 or mac")
	}
}
