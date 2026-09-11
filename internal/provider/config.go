package provider

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

type providerModel struct {
	AllowAAAChanges  types.Bool     `tfsdk:"allow_aaa_changes"`
	Host             types.String   `tfsdk:"host"`
	Username         types.String   `tfsdk:"username"`
	Password         types.String   `tfsdk:"password"`
	Transport        types.String   `tfsdk:"transport"`
	OperationTimeout types.String   `tfsdk:"operation_timeout"`
	PersistenceMode  types.String   `tfsdk:"persistence_mode"`
	RESTCONF         *restconfModel `tfsdk:"restconf"`
	SSH              *sshModel      `tfsdk:"ssh"`
}

type restconfModel struct {
	Enabled            types.Bool   `tfsdk:"enabled"`
	Port               types.Int64  `tfsdk:"port"`
	BasePath           types.String `tfsdk:"base_path"`
	CA                 types.String `tfsdk:"ca_certificate"`
	Certificate        types.String `tfsdk:"client_certificate"`
	Key                types.String `tfsdk:"client_key"`
	InsecureSkipVerify types.Bool   `tfsdk:"insecure_skip_verify"`
}
type sshModel struct {
	Enabled        types.Bool   `tfsdk:"enabled"`
	Port           types.Int64  `tfsdk:"port"`
	PrivateKey     types.String `tfsdk:"private_key"`
	KnownHosts     types.String `tfsdk:"known_hosts"`
	EnablePassword types.String `tfsdk:"enable_password"`
}

func providerSchema() schema.Schema {
	return schema.Schema{
		Description: "Manage FastIron switch configuration. SSH discovers firmware and persists writes; RESTCONF is preferred for supported configuration operations.",
		Attributes: map[string]schema.Attribute{
			"allow_aaa_changes": schema.BoolAttribute{Optional: true, Description: "Permit AAA resource changes (default false). AAA changes can affect access to the switch."},
			"host":              schema.StringAttribute{Optional: true, Description: "Switch hostname or IP address, without scheme or port. Defaults to FASTIRON_HOST."},
			"username":          schema.StringAttribute{Optional: true, Description: "Automation username. Defaults to FASTIRON_USERNAME."},
			"password":          schema.StringAttribute{Optional: true, Sensitive: true, Description: "Authentication password. Defaults to FASTIRON_PASSWORD."},
			"transport":         schema.StringAttribute{Optional: true, Description: "auto (default), restconf, or ssh. Unsupported operations fail before mutation."},
			"operation_timeout": schema.StringAttribute{Optional: true, Description: "Maximum duration of one transport operation. Defaults to 30s."},
			"persistence_mode":  schema.StringAttribute{Optional: true, Description: "after_each_write (default), manual, or never. Manual mode requires an explicit configuration_save resource."},
		},
		Blocks: map[string]schema.Block{
			"restconf": schema.SingleNestedBlock{Attributes: map[string]schema.Attribute{
				"enabled":              schema.BoolAttribute{Optional: true, Description: "Enable RESTCONF (default true)."},
				"port":                 schema.Int64Attribute{Optional: true, Description: "HTTPS port (default 443)."},
				"base_path":            schema.StringAttribute{Optional: true, Description: "RESTCONF data base path (default /restconf/data)."},
				"ca_certificate":       schema.StringAttribute{Optional: true, Description: "Additional trusted PEM CA certificates."},
				"client_certificate":   schema.StringAttribute{Optional: true, Description: "PEM client certificate for mutual TLS."},
				"client_key":           schema.StringAttribute{Optional: true, Sensitive: true, Description: "PEM private key for mutual TLS."},
				"insecure_skip_verify": schema.BoolAttribute{Optional: true, Description: "Disable TLS certificate verification (default false)."},
			}},
			"ssh": schema.SingleNestedBlock{Attributes: map[string]schema.Attribute{
				"enabled":         schema.BoolAttribute{Optional: true, Description: "Enable SSH (default true)."},
				"port":            schema.Int64Attribute{Optional: true, Description: "SSH port (default 22)."},
				"private_key":     schema.StringAttribute{Optional: true, Sensitive: true, Description: "PEM private key. Defaults to FASTIRON_SSH_PRIVATE_KEY."},
				"known_hosts":     schema.StringAttribute{Optional: true, Description: "Trusted known_hosts file contents. Defaults to FASTIRON_KNOWN_HOSTS; host keys are always verified."},
				"enable_password": schema.StringAttribute{Optional: true, Sensitive: true, Description: "Privilege elevation password. Defaults to FASTIRON_ENABLE_PASSWORD."},
			}},
		},
	}
}

func (p *fastironProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var model providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cfg, err := model.config()
	if err != nil {
		resp.Diagnostics.AddError("Invalid FastIron provider configuration", err.Error())
		return
	}
	device, err := fastiron.New(cfg)
	if err != nil {
		resp.Diagnostics.AddError("Cannot configure FastIron client", err.Error())
		return
	}
	resp.ResourceData = device
	resp.DataSourceData = device
}

func (m providerModel) config() (fastiron.Config, error) {
	var err error
	str := func(v types.String, env, def string) string {
		if v.IsUnknown() {
			err = errors.New("provider settings must be known before configuring the switch")
			return ""
		}
		if !v.IsNull() {
			return v.ValueString()
		}
		if env != "" {
			if value, ok := os.LookupEnv(env); ok {
				return value
			}
		}
		return def
	}
	boolean := func(v types.Bool, def bool) bool {
		if v.IsUnknown() {
			err = errors.New("provider settings must be known before configuring the switch")
		}
		if v.IsNull() {
			return def
		}
		return v.ValueBool()
	}
	port := func(v types.Int64, def int64) string {
		if v.IsUnknown() {
			err = errors.New("provider settings must be known before configuring the switch")
		}
		n := v.ValueInt64()
		if v.IsNull() {
			n = def
		}
		if n < 1 || n > 65535 {
			err = errors.New("transport ports must be between 1 and 65535")
		}
		return strconv.FormatInt(n, 10)
	}
	host := str(m.Host, "FASTIRON_HOST", "")
	if host == "" || strings.ContainsAny(host, "/\\@?# \t\r\n") || (strings.Contains(host, ":") && net.ParseIP(host) == nil) {
		return fastiron.Config{}, errors.New("host must be a hostname or IP address without scheme, port, or credentials")
	}
	user := str(m.Username, "FASTIRON_USERNAME", "")
	if user == "" {
		return fastiron.Config{}, errors.New("username or FASTIRON_USERNAME is required")
	}
	password := str(m.Password, "FASTIRON_PASSWORD", "")
	timeout, parseErr := time.ParseDuration(str(m.OperationTimeout, "", "30s"))
	if parseErr != nil || timeout <= 0 {
		return fastiron.Config{}, errors.New("operation_timeout must be a positive duration")
	}
	cfg := fastiron.Config{AllowAAAChanges: boolean(m.AllowAAAChanges, false), Host: host, Transport: str(m.Transport, "", "auto"), Persistence: str(m.PersistenceMode, "", "after_each_write")}
	r := m.RESTCONF
	if r == nil {
		r = &restconfModel{}
	}
	if boolean(r.Enabled, true) {
		base := str(r.BasePath, "", "/restconf/data")
		if !strings.HasPrefix(base, "/") || strings.ContainsAny(base, "?#\\\r\n") {
			return cfg, errors.New("RESTCONF base_path must be an absolute URL path")
		}
		cfg.RESTCONF = &restconf.Config{URL: "https://" + net.JoinHostPort(host, port(r.Port, 443)) + base, Username: user, Password: password, CA: str(r.CA, "", ""), Certificate: str(r.Certificate, "", ""), Key: str(r.Key, "", ""), InsecureSkipVerify: boolean(r.InsecureSkipVerify, false), Timeout: timeout}
	}
	s := m.SSH
	if s == nil {
		s = &sshModel{}
	}
	if boolean(s.Enabled, true) {
		cfg.SSH = &ssh.Config{Address: net.JoinHostPort(host, port(s.Port, 22)), Username: user, Password: password, PrivateKey: str(s.PrivateKey, "FASTIRON_SSH_PRIVATE_KEY", ""), KnownHosts: str(s.KnownHosts, "FASTIRON_KNOWN_HOSTS", ""), EnablePassword: str(s.EnablePassword, "FASTIRON_ENABLE_PASSWORD", ""), Timeout: timeout}
	}
	return cfg, err
}
