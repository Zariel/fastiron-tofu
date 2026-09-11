package provider

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func configuredSecretChecksum(value types.String) []byte {
	if value.IsNull() {
		return []byte("null")
	}
	checksum := sha256.Sum256([]byte(value.ValueString()))
	encoded, _ := json.Marshal(fmt.Sprintf("%x", checksum))
	return encoded
}
