package igmp

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	vlanDataSource struct {
		device     *fastiron.Device
		collection bool
	}
	vlanQuery struct {
		VLANID  types.Int64  `tfsdk:"vlan_id"`
		Mode    types.String `tfsdk:"querier_mode"`
		Version types.Int64  `tfsdk:"version"`
	}
)

func NewVLANDataSource() *vlanDataSource { return &vlanDataSource{} }

func NewInventoryDataSource() *vlanDataSource { return &vlanDataSource{collection: true} }

var _ datasource.DataSourceWithConfigure = (*vlanDataSource)(nil)

func (d *vlanDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vlan_igmp_snooping"
	if d.collection {
		resp.TypeName = req.ProviderTypeName + "_igmp_snooping_vlans"
	}
}

func (d *vlanDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"vlan_id":      schema.Int64Attribute{Required: !d.collection, Computed: d.collection, Description: "Existing VLAN identifier, 1 through 4095."},
		"querier_mode": schema.StringAttribute{Computed: true, Description: "Configured active, passive or disabled override. Null means no VLAN mode override; it does not establish whether snooping is operational."},
		"version":      schema.Int64Attribute{Computed: true, Description: "Configured version 2 or 3 override. Null means no VLAN version override."},
	}
	description := "Reads native IGMP snooping overrides on an existing VLAN without taking ownership or saving configuration. Null fields indicate absent VLAN overrides."
	if d.collection {
		attributes = map[string]schema.Attribute{
			"vlans": schema.MapNestedAttribute{Computed: true, Description: "Native VLANs keyed by decimal VLAN ID, including the default VLAN and VLANs without overrides.", NestedObject: schema.NestedAttributeObject{Attributes: attributes}},
		}
		description = "Reads native IGMP snooping overrides for every configured VLAN without taking ownership or saving configuration. Includes CLI-only overrides omitted by RESTCONF; null fields indicate absent VLAN overrides."
	}
	resp.Schema = schema.Schema{Description: description, Attributes: attributes}
}

func (d *vlanDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *vlanDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.collection {
		observed, err := readInventory(ctx, d.device)
		if err != nil {
			resp.Diagnostics.AddError("Cannot read VLAN IGMP inventory", err.Error())
			return
		}
		vlans := make(map[string]vlanQuery, len(observed))
		for id, current := range observed {
			vlans[strconv.FormatInt(id, 10)] = queryState(id, current)
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, struct {
			VLANs map[string]vlanQuery `tfsdk:"vlans"`
		}{VLANs: vlans})...)
		return
	}

	var query vlanQuery
	resp.Diagnostics.Append(req.Config.Get(ctx, &query)...)
	if resp.Diagnostics.HasError() {
		return
	}

	observed, err := read(ctx, d.device, query.VLANID.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read VLAN IGMP overrides", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, queryState(query.VLANID.ValueInt64(), observed.settings))...)
}

func queryState(id int64, observed settings) vlanQuery {
	query := vlanQuery{VLANID: types.Int64Value(id), Mode: types.StringNull(), Version: types.Int64Null()}
	if observed.Mode != "" {
		query.Mode = types.StringValue(observed.Mode)
	}
	if observed.Version != 0 {
		query.Version = types.Int64Value(observed.Version)
	}
	return query
}
