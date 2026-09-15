// Package configtest reads complete native configuration fixtures for tests.
package configtest

import (
	"testing"

	"github.com/zariel/fastiron-tofu/internal/config"
)

func Parse(t testing.TB, text string) *config.Document {
	t.Helper()
	document, err := config.Parse(text)
	if err != nil {
		t.Fatalf("parse configuration fixture: %v", err)
	}
	return document
}
