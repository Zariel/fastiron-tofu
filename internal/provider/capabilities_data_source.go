package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type capabilitiesDataSource struct{ device *fastiron.Device }
type capabilitiesModel struct {
	Firmware  types.String `tfsdk:"firmware"`
	BootImage types.String `tfsdk:"boot_image"`
}

func (d *capabilitiesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_capabilities"
}
func (d *capabilitiesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads active firmware evidence over SSH. Firmware compatibility does not establish availability of every hardware or licensed feature.", Attributes: map[string]schema.Attribute{
		"firmware":   schema.StringAttribute{Computed: true, Description: "Active FastIron firmware version, excluding the platform build suffix."},
		"boot_image": schema.StringAttribute{Computed: true, Description: "Active image label when present in show version output; empty otherwise."},
	}}
}
func (d *capabilitiesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}
func (d *capabilitiesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	c, err := d.device.Discover(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot discover firmware", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, capabilitiesModel{Firmware: types.StringValue(c.Firmware), BootImage: types.StringValue(c.BootImage)})...)
}
