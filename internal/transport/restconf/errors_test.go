package restconf

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMissingInstance(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		missing    bool
	}{
		{"FastIron absent list entry", `{"ietf-restconf:errors":{"error":[{"error-tag":"invalid-value","error-app-tag":"data-invalid","error-message":"unknown resource instance","error-info":{"error-number":388}}]}}`, true},
		{"unrelated validation", `{"ietf-restconf:errors":{"error":[{"error-tag":"invalid-value","error-app-tag":"data-invalid","error-info":{"error-number":274}}]}}`, false},
		{"malformed response", `not-json`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400); fmt.Fprint(w, tc.body) }))
			defer s.Close()
			err := testClient(t, s).Do(context.Background(), "GET", "/vlans/vlan=3053", nil, nil)
			if errors.Is(err, ErrNotFound) != tc.missing {
				t.Fatalf("absence classification: %v", err)
			}
		})
	}
}
