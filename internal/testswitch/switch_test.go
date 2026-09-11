package testswitch_test

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/testswitch"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
	"github.com/zariel/fastiron-tofu/internal/transport/ssh"
)

func TestHandlers(t *testing.T) {
	s := testswitch.New(t, func(command string) string {
		switch command {
		case "skip-page-display":
			return ""
		case "show running-config":
			return "ver 09.0.10kT213\n!\nvlan 53 name INFRA by port\n!\nend"
		default:
			return "% Invalid input"
		}
	})
	s.HandleFunc("GET /restconf/data/interfaces/{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("name") != "interface=ethernet 1/1/9" {
			t.Errorf("interface key = %q", r.PathValue("name"))
		}
		fmt.Fprint(w, `{"enabled":true}`)
	})
	s.HandleFunc("PATCH /restconf/data/interfaces/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.REST.Certificate().Raw})
	rest, err := restconf.New(restconf.Config{URL: s.REST.URL + "/restconf/data", Username: "automation", Password: "test", CA: string(ca), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Enabled bool }
	if err := rest.Do(context.Background(), http.MethodGet, "/interfaces/interface=ethernet%201%2F1%2F9", nil, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Fatal("registered GET response was not returned")
	}
	err = rest.Do(context.Background(), http.MethodPatch, "/interfaces/interface=ethernet%201%2F1%2F9", map[string]bool{"enabled": false}, nil)
	var responseError *restconf.HTTPError
	if !errors.As(err, &responseError) || responseError.Status != http.StatusServiceUnavailable {
		t.Fatalf("registered PATCH failure = %v; want HTTP 503", err)
	}

	shell, err := ssh.New(ssh.Config{Address: s.SSHAddress, Username: "automation", Password: "test", KnownHosts: s.KnownHosts, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	output, err := shell.Run(context.Background(), false, "show running-config")
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 1 || output[0] != "ver 09.0.10kT213\n!\nvlan 53 name INFRA by port\n!\nend" {
		t.Fatalf("native output = %q", output)
	}
}
