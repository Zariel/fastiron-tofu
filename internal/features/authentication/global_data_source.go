package authentication

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type GlobalDataSource struct{ device *fastiron.Device }

func (d *GlobalDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_authentication"
}

func (d *GlobalDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads global FlexAuth configuration. Per-port configuration and AAA server policy are separate. Requires read-only SSH access for native configuration verification.", Attributes: map[string]schema.Attribute{
		"dot1x_enabled":              schema.BoolAttribute{Computed: true, Description: "Whether dot1x is enabled globally."},
		"mac_authentication_enabled": schema.BoolAttribute{Computed: true, Description: "Whether MAC authentication is enabled globally."},
		"auth_order":                 schema.StringAttribute{Computed: true, Description: "Authentication sequence: dot1x mac-auth (default) or mac-auth dot1x."},
		"auth_default_vlan":          schema.Int64Attribute{Computed: true, Description: "Authentication default VLAN; null when unconfigured."},
		"restricted_vlan":            schema.Int64Attribute{Computed: true, Description: "Restricted VLAN; null when unconfigured."},
		"critical_vlan":              schema.Int64Attribute{Computed: true, Description: "Critical VLAN; null when unconfigured."},
		"voice_vlan":                 schema.Int64Attribute{Computed: true, Description: "Voice VLAN; null when unconfigured."},
		"guest_vlan":                 schema.Int64Attribute{Computed: true, Description: "Dot1x guest VLAN; null when unconfigured."},
		"max_sessions":               schema.Int64Attribute{Computed: true, Description: "Global maximum authentication sessions; defaults to 2."},
		"re_authentication":          schema.BoolAttribute{Computed: true, Description: "Whether periodic reauthentication is enabled."},
		"mac_dot1x_disable":          schema.BoolAttribute{Computed: true, Description: "Skip dot1x after successful MAC authentication when MAC authentication is first."},
		"mac_dot1x_override":         schema.BoolAttribute{Computed: true, Description: "Attempt dot1x after failed MAC authentication with MAC-first order and restricted-VLAN failure handling."},
		"failure_action":             schema.StringAttribute{Computed: true, Description: "Configured auth-fail-action arguments, including any voice VLAN variant. Null means no configured action (block traffic by default)."},
		"timeout_action":             schema.StringAttribute{Computed: true, Description: "Configured auth-timeout-action arguments, including any voice VLAN variant. Null means no configured action; it is distinct from explicit failure."},
	}}
}

func (d *GlobalDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *GlobalDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	current, err := readGlobal(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read global authentication", err.Error())
		return
	}
	model := struct {
		Dot1XEnabled     bool         `tfsdk:"dot1x_enabled"`
		MACEnabled       bool         `tfsdk:"mac_authentication_enabled"`
		AuthOrder        string       `tfsdk:"auth_order"`
		DefaultVLAN      types.Int64  `tfsdk:"auth_default_vlan"`
		RestrictedVLAN   types.Int64  `tfsdk:"restricted_vlan"`
		CriticalVLAN     types.Int64  `tfsdk:"critical_vlan"`
		VoiceVLAN        types.Int64  `tfsdk:"voice_vlan"`
		GuestVLAN        types.Int64  `tfsdk:"guest_vlan"`
		MaxSessions      int64        `tfsdk:"max_sessions"`
		Reauthentication bool         `tfsdk:"re_authentication"`
		MACDot1XDisable  bool         `tfsdk:"mac_dot1x_disable"`
		MACDot1XOverride bool         `tfsdk:"mac_dot1x_override"`
		FailureAction    types.String `tfsdk:"failure_action"`
		TimeoutAction    types.String `tfsdk:"timeout_action"`
	}{
		Dot1XEnabled: current.Dot1XEnabled, MACEnabled: current.MACEnabled, AuthOrder: current.AuthOrder,
		MaxSessions: current.MaxSessions, Reauthentication: current.Reauthentication,
		MACDot1XDisable: current.MACDot1XDisable, MACDot1XOverride: current.MACDot1XOverride,
		FailureAction: types.StringNull(), TimeoutAction: types.StringNull(),
	}
	for target, value := range map[*types.Int64]int64{
		&model.DefaultVLAN: current.DefaultVLAN, &model.RestrictedVLAN: current.RestrictedVLAN,
		&model.CriticalVLAN: current.CriticalVLAN, &model.VoiceVLAN: current.VoiceVLAN, &model.GuestVLAN: current.GuestVLAN,
	} {
		*target = types.Int64Null()
		if value != 0 {
			*target = types.Int64Value(value)
		}
	}
	if current.FailureAction != "" {
		model.FailureAction = types.StringValue(current.FailureAction)
	}
	if current.TimeoutAction != "" {
		model.TimeoutAction = types.StringValue(current.TimeoutAction)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}
