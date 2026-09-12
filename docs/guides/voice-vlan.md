# Interface voice VLAN

`fastiron_interface_voice_vlan` owns the local IP voice VLAN on one Ethernet interface. It changes only the interface's `voice-vlan` policy. VLAN creation and membership, port name, administrative state, PoE, FlexAuth and LLDP-MED settings remain separately owned.

```hcl
resource "fastiron_interface_voice_vlan" "phone" {
  interface = "ethernet 1/1/12"
  vlan_id   = 3053
}
```

Use a canonical `ethernet <stack>/<slot>/<port>` name and a VLAN ID from 1 through 4095. Changing the interface replaces the resource. The policy can reference a VLAN that does not yet exist; create the VLAN and configure any required membership separately. Acceptance of the policy does not establish phone connectivity.

Deleting the resource removes the local policy. It does not restore the value that preceded management. Only one resource should manage this policy on a given interface.

Import an existing local policy using its interface name:

```sh
tofu import fastiron_interface_voice_vlan.phone 'ethernet 1/1/12'
```

To query the local policy without managing it:

```hcl
data "fastiron_interface_voice_vlan" "phone" {
  interface = "ethernet 1/1/12"
}

output "voice_vlan" {
  value = data.fastiron_interface_voice_vlan.phone.vlan_id
}
```

The data source returns `null` when no local policy is configured. It reports configuration, not the VLAN currently used by a connected phone. Queries and imports do not save configuration.

Configuration uses the RESTCONF Ethernet `ip-voice-vlan` leaf. SSH access is also required to read native configuration and verify writes. Reads use native configuration because the RESTCONF cache can retain values after CLI changes. Updating a drifted policy can briefly remove the local setting before applying the desired value.

With `persistence_mode = "after_each_write"`, mutations save configuration and verify that startup matches running configuration. A failed mutation or save sets `persistence_pending`; retry the operation to reconcile and save. A failed deletion retains resource state until persistence succeeds, even when the local policy is already absent.

This resource does not enable FlexAuth or configure its global voice VLAN and action variants. It also does not configure LLDP-MED network policies.

See the [complete example](../../examples/voice-vlan/main.tf) for provider configuration and a separately managed VLAN. Configure VLAN membership according to the phone and network requirements.
