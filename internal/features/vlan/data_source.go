package vlan

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type dataSource struct {
	device     *fastiron.Device
	collection bool
}

type vlanStatus struct {
	ID   int64  `tfsdk:"vlan_id"`
	Name string `tfsdk:"name"`
}

func NewDataSource() *dataSource { return &dataSource{} }

func NewCollectionDataSource() *dataSource { return &dataSource{collection: true} }

func (d *dataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vlan"
	if d.collection {
		resp.TypeName += "s"
	}
}

func (d *dataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"vlan_id": schema.Int64Attribute{Required: !d.collection, Computed: d.collection, Description: "VLAN ID from 1 through 4094. Includes the default VLAN, which the resource does not manage."},
		"name":    schema.StringAttribute{Computed: true, Description: "Reported VLAN name, or an empty string when unnamed."},
	}
	description := "Reads a VLAN's ID and name through RESTCONF without taking ownership. A missing VLAN produces a diagnostic."
	if d.collection {
		attributes = map[string]schema.Attribute{
			"vlans": schema.MapNestedAttribute{Computed: true, Description: "VLANs keyed by decimal VLAN ID, including the default VLAN.", NestedObject: schema.NestedAttributeObject{Attributes: attributes}},
		}
		description = "Reads VLAN IDs and names through RESTCONF without changing configuration. Memberships and additional VLAN policies are outside this query."
	}
	resp.Schema = schema.Schema{Description: description, Attributes: attributes}
}

func (d *dataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (d *dataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.collection {
		vlans, err := readAll(ctx, d.device)
		if err != nil {
			resp.Diagnostics.AddError("Cannot read VLANs", err.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, struct {
			VLANs map[string]vlanStatus `tfsdk:"vlans"`
		}{VLANs: vlans})...)
		return
	}
	var id types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("vlan_id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if id.IsUnknown() || id.IsNull() {
		resp.Diagnostics.AddError("Unknown VLAN ID", "vlan_id must be known before reading.")
		return
	}
	current, err := Read(ctx, d.device, id.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read VLAN", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, vlanStatus{ID: current.ID, Name: current.Name})...)
}

func readAll(ctx context.Context, device *fastiron.Device) (map[string]vlanStatus, error) {
	if !device.RESTCONFEnabled() {
		return nil, errors.New("RESTCONF is required for VLAN discovery")
	}
	var response struct {
		VLANs *struct {
			VLAN []vlanEntry `json:"vlan"`
		} `json:"openconfig-network-instance:vlans"`
	}
	if err := device.DoREST(ctx, http.MethodGet, vlanPath, nil, &response); err != nil {
		return nil, err
	}
	if response.VLANs == nil {
		return nil, errors.New("RESTCONF VLAN collection is missing its configuration container")
	}
	vlans := map[string]vlanStatus{}
	for _, entry := range response.VLANs.VLAN {
		if entry.ID < 1 || entry.ID > 4094 || entry.Config.ID != entry.ID {
			return nil, errors.New("RESTCONF VLAN collection contains an invalid or inconsistent identity")
		}
		key := strconv.FormatInt(entry.ID, 10)
		if _, exists := vlans[key]; exists {
			return nil, errors.New("RESTCONF VLAN collection contains duplicate identities")
		}
		vlans[key] = vlanStatus{ID: entry.ID, Name: entry.Config.Name}
	}
	return vlans, nil
}
