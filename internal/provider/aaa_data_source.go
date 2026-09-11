package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type aaaDataSource struct{ device *fastiron.Device }

func (d *aaaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aaa"
}

func (d *aaaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads AAA login methods, dot1x default authentication and CoA policy without exposing account credentials or server keys.", Attributes: map[string]schema.Attribute{
		"login_methods": schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Configured login authentication methods, in attempt order."},
		"dot1x_default": schema.StringAttribute{Computed: true, Description: "Default dot1x authentication reported by the switch; an absent RESTCONF policy is normalized to none."},
		"coa_enabled":   schema.BoolAttribute{Computed: true, Description: "Whether RADIUS Change of Authorization is enabled."},
		"coa_ignore":    schema.SetAttribute{Computed: true, ElementType: types.StringType, Description: "CoA actions ignored by the switch."},
	}}
}

func (d *aaaDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *aaaDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	policy, err := d.device.AAAPolicy(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read AAA policy", err.Error())
		return
	}
	dot1x := policy.Dot1XDefault
	if dot1x == "" {
		dot1x = "none"
	}
	model := struct {
		LoginMethods []string `tfsdk:"login_methods"`
		Dot1XDefault string   `tfsdk:"dot1x_default"`
		CoAEnabled   bool     `tfsdk:"coa_enabled"`
		CoAIgnore    []string `tfsdk:"coa_ignore"`
	}{policy.LoginMethods, dot1x, policy.CoAEnabled, policy.CoAIgnore}
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}
