package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestProviderConfig(t *testing.T) {
	t.Setenv("FASTIRON_HOST", "2001:db8::1")
	t.Setenv("FASTIRON_USERNAME", "environment-user")
	t.Setenv("FASTIRON_PASSWORD", "environment-password")
	t.Setenv("FASTIRON_KNOWN_HOSTS", "trusted-host-keys")
	cfg, err := (providerModel{}).config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AllowAAAChanges || cfg.Transport != "auto" || cfg.Persistence != "after_each_write" || cfg.SSH.Address != "[2001:db8::1]:22" || cfg.RESTCONF.URL != "https://[2001:db8::1]:443/restconf/data" || cfg.SSH.KnownHosts != "trusted-host-keys" {
		t.Fatalf("incorrect defaults: %#v", cfg)
	}
	cfg, err = (providerModel{AllowAAAChanges: types.BoolValue(true), Username: types.StringValue("explicit-user"), Password: types.StringValue("")}).config()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowAAAChanges || cfg.SSH.Username != "explicit-user" || cfg.SSH.Password != "" {
		t.Fatal("environment overrode explicit configuration")
	}
	for _, model := range []providerModel{
		{Host: types.StringValue("https://switch")},
		{Host: types.StringValue("switch:443")},
		{Host: types.StringValue("fe80::1%eth0")},
		{OperationTimeout: types.StringValue("0s")},
		{Host: types.StringUnknown()},
		{AllowAAAChanges: types.BoolUnknown()},
		{SSH: &sshModel{Port: types.Int64Value(65536)}},
		{RESTCONF: &restconfModel{Enabled: types.BoolUnknown()}},
	} {
		if _, err := model.config(); err == nil {
			t.Fatalf("accepted invalid configuration: %#v", model)
		}
	}
}
