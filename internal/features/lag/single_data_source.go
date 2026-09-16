package lag

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type singleDataSource struct{ device *fastiron.Device }

type singleQueryModel struct {
	ID      types.String `tfsdk:"id"`
	LAGID   types.Int64  `tfsdk:"lag_id"`
	Name    types.String `tfsdk:"name"`
	Mode    types.String `tfsdk:"mode"`
	Members types.Set    `tfsdk:"members"`
}

func NewSingleDataSource() *singleDataSource { return &singleDataSource{} }

var _ datasource.DataSourceWithConfigure = (*singleDataSource)(nil)

func (d *singleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lag"
}

func (d *singleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads one native aggregate and its Ethernet members without writing or saving. Requires RESTCONF and SSH access; a missing native LAG is an error.", Attributes: map[string]schema.Attribute{
		"id":      schema.StringAttribute{Computed: true, Description: "Canonical interface identity, such as lag 5."},
		"lag_id":  schema.Int64Attribute{Required: true, Description: "Positive LAG identifier."},
		"name":    schema.StringAttribute{Computed: true, Description: "Configured aggregate name, separate from interface descriptions and member names."},
		"mode":    schema.StringAttribute{Computed: true, Description: "dynamic (LACP) or static."},
		"members": schema.SetAttribute{Computed: true, ElementType: types.StringType, Description: "Complete set of canonical Ethernet member names; empty for an empty aggregate. Membership does not indicate link readiness."},
	}}
}

func (d *singleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *singleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state singleQueryModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.LAGID.ValueInt64()
	if id < 1 {
		resp.Diagnostics.AddError("Invalid LAG identity", "lag_id must be positive.")
		return
	}
	if _, err := d.device.Discover(ctx); err != nil {
		resp.Diagnostics.AddError("Cannot discover switch", err.Error())
		return
	}
	observed, err := readLAG(ctx, d.device, id)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LAG", err.Error())
		return
	}

	state.ID = types.StringValue("lag " + strconv.FormatInt(observed.ID, 10))
	state.Name = types.StringValue(observed.Name)
	state.Mode = types.StringValue(observed.Mode)
	members, diags := types.SetValueFrom(ctx, types.StringType, observed.Members)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Members = members
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
