package jumbo

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type dataSource struct{ device *fastiron.Device }

func NewDataSource() *dataSource { return &dataSource{} }

var _ datasource.DataSourceWithConfigure = (*dataSource)(nil)

func (d *dataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_jumbo"
}

func (d *dataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured global jumbo-frame support without taking ownership or saving configuration.", Attributes: map[string]schema.Attribute{
		"enabled": schema.BoolAttribute{Computed: true, Description: "Whether the native global jumbo command is configured. This does not measure forwarded frame sizes or report a per-interface MTU."},
	}}
}

func (d *dataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	device, ok := req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
		return
	}
	d.device = device
}

func (d *dataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	observed, err := read(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read jumbo configuration", err.Error())
		return
	}
	state := struct {
		Enabled types.Bool `tfsdk:"enabled"`
	}{Enabled: types.BoolValue(observed.enabled)}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
