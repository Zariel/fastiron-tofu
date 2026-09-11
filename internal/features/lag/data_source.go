package lag

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	DataSource struct{ device *fastiron.Device }
	lagsModel  struct {
		LAGs map[string]lagStatusModel `tfsdk:"lags"`
	}
	lagStatusModel struct {
		ID      types.Int64  `tfsdk:"lag_id"`
		Name    types.String `tfsdk:"name"`
		Mode    types.String `tfsdk:"mode"`
		Members types.Set    `tfsdk:"members"`
	}
)

func (d *DataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_lags"
}

func (d *DataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Reads configured LAGs and their Ethernet members.", Attributes: map[string]schema.Attribute{
		"lags": schema.MapNestedAttribute{Computed: true, Description: "LAG configuration keyed by canonical interface name, such as lag 5.", NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"lag_id":  schema.Int64Attribute{Computed: true, Description: "Numeric LAG identity."},
			"name":    schema.StringAttribute{Computed: true, Description: "Configured LAG name."},
			"mode":    schema.StringAttribute{Computed: true, Description: "dynamic (LACP) or static."},
			"members": schema.SetAttribute{Computed: true, ElementType: types.StringType, Description: "Canonical Ethernet interface names belonging to the LAG."},
		}}},
	}}
}

func (d *DataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *DataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	lags, err := readLAGs(ctx, d.device)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read LAGs", err.Error())
		return
	}
	state := lagsModel{LAGs: map[string]lagStatusModel{}}
	for _, lag := range lags {
		members, diags := types.SetValueFrom(ctx, types.StringType, lag.Members)
		resp.Diagnostics.Append(diags...)
		state.LAGs["lag "+strconv.FormatInt(lag.ID, 10)] = lagStatusModel{ID: types.Int64Value(lag.ID), Name: types.StringValue(lag.Name), Mode: types.StringValue(lag.Mode), Members: members}
	}
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	}
}
