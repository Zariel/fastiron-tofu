package provider

import (
	"bytes"
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
)

type (
	userResource      struct{ device *fastiron.Device }
	userResourceModel struct {
		ID                 types.String `tfsdk:"id"`
		Username           types.String `tfsdk:"username"`
		Privilege          types.Int64  `tfsdk:"privilege"`
		Password           types.String `tfsdk:"password"`
		PersistencePending types.Bool   `tfsdk:"persistence_pending"`
	}
)

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aaa_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Owns one local account's username, privilege and password. Requires allow_aaa_changes. Import with username <name>. Accounts used by provider transports cannot be modified.", Attributes: map[string]schema.Attribute{
		"id":                  schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"username":            schema.StringAttribute{Required: true, Description: "Account name, 1–48 non-whitespace ASCII characters. Changing it replaces the account.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"privilege":           schema.Int64Attribute{Required: true, Description: "0: super user; 4: port configuration; 5: read only; 6: cloud user; 7: no syslog access. Availability depends on firmware."},
		"password":            schema.StringAttribute{Required: true, Sensitive: true, Description: "Configured password, 1–48 bytes without control characters. Stored as sensitive configuration in state; returned password hashes are never exposed."},
		"persistence_pending": schema.BoolAttribute{Computed: true, Description: "True when an operation still requires reconciliation or persistence."},
	}}
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.device, ok = req.ProviderData.(*fastiron.Device)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider client", "Expected a FastIron device client.")
	}
}

func (m userResourceModel) user() fastiron.User {
	return fastiron.User{Username: m.Username.ValueString(), Privilege: m.Privilege.ValueInt64()}
}

func (m *userResourceModel) observe(u fastiron.User) {
	m.ID = types.StringValue("username " + u.Username)
	m.Username = types.StringValue(u.Username)
	m.Privilege = types.Int64Value(u.Privilege)
}

func (r *userResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if r.device != nil {
		if err := r.device.CheckAAAChanges(); err != nil {
			resp.Diagnostics.AddError("AAA changes disabled", err.Error())
			return
		}
	}
	if req.Plan.Raw.IsNull() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), false)...)
	var m userResourceModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.Username.IsUnknown() && !m.Privilege.IsUnknown() {
		if err := fastiron.ValidateUser(m.user()); err != nil {
			resp.Diagnostics.AddError("Invalid local user", err.Error())
		}
	}
	if !req.State.Raw.IsNull() && !m.Password.IsUnknown() {
		applied, diags := req.Private.GetKey(ctx, "configured_password")
		resp.Diagnostics.Append(diags...)
		if !bytes.Equal(applied, configuredSecretChecksum(m.Password)) {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("persistence_pending"), types.BoolUnknown())...)
		}
	}
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m userResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyUser(ctx, m.user(), m.Password.ValueString(), true)
	if observed != nil {
		m.observe(*observed)
		m.PersistencePending = types.BoolValue(err != nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot create local user", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, "configured_password", configuredSecretChecksum(m.Password))...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m userResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	observed, err := r.device.ApplyUser(ctx, m.user(), m.Password.ValueString(), true)
	if observed != nil {
		m.observe(*observed)
	}
	m.PersistencePending = types.BoolValue(err != nil)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update local user", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.Private.SetKey(ctx, "configured_password", configuredSecretChecksum(m.Password))...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m userResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	users, err := r.device.Users(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read local user", err.Error())
		return
	}
	for _, u := range users {
		if u.Username == m.Username.ValueString() {
			m.observe(u)
			if m.PersistencePending.IsNull() {
				m.PersistencePending = types.BoolValue(false)
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
			return
		}
	}
	if !m.PersistencePending.ValueBool() {
		resp.State.RemoveResource(ctx)
	}
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m userResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.device.ApplyUser(ctx, m.user(), "", false); err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("persistence_pending"), true)...)
		resp.Diagnostics.AddError("Cannot delete local user", err.Error())
	}
}

func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name, ok := strings.CutPrefix(req.ID, "username ")
	u := fastiron.User{Username: name}
	if !ok || fastiron.ValidateUser(u) != nil {
		resp.Diagnostics.AddError("Invalid local user identity", "Use username <name>.")
		return
	}
	m := userResourceModel{Password: types.StringNull(), PersistencePending: types.BoolValue(false)}
	m.observe(u)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
