package ethernet

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zariel/fastiron-tofu/internal/fastiron"
	"github.com/zariel/fastiron-tofu/internal/transport/restconf"
)

func TestEmptyInterfaceDatabase(t *testing.T) {
	// A RESTCONF restart returned these shapes despite native physical and
	// logical interfaces still existing. They cannot establish resource absence.
	for _, body := range []string{
		`{"openconfig-interfaces:interfaces":{}}`,
		`{"openconfig-interfaces:interfaces":{"interface":[]}}`,
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, body)
		}))
		t.Cleanup(server.Close)
		device, err := fastiron.New(fastiron.Config{Host: server.URL, Transport: "restconf", Persistence: "manual", RESTCONF: &restconf.Config{URL: server.URL, InsecureSkipVerify: true, Timeout: time.Second}})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := Read(context.Background(), device, "1/1/7"); err == nil || errors.Is(err, fastiron.ErrNotFound) {
			t.Fatalf("incomplete database reported as confirmed state: %v", err)
		}
	}
}
