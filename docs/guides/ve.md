# Routed VLAN interfaces

`fastiron_interface_ve` owns VE existence and its port name. The parent VLAN must exist, and `ve_id` and `vlan_id` must match the FastIron routed VLAN identity. Omission of `port_name` clears the description.

```hcl
resource "fastiron_vlan" "transit" {
  vlan_id = 3053
}

resource "fastiron_interface_ve" "transit" {
  ve_id     = 3053
  vlan_id   = fastiron_vlan.transit.vlan_id
  port_name = "TRANSIT"
}
```

Import with `tofu import fastiron_interface_ve.transit 've 3053'`. The resource exposes the canonical `name`, for example `ve 3053`, for independently managed address and protocol resources.

Reads require SSH access to native configuration as well as RESTCONF. Native configuration determines VE existence and its port name because RESTCONF can retain an old name or interface after CLI changes. An unreadable or malformed interface collection is an error. Duplicate VE entries or inconsistent VLAN bindings are also rejected.

Name updates wait for RESTCONF configuration to synchronize with native state before writing: a request that matches a stale cached name can return success without changing the switch. Omission uses the narrow description DELETE operation, which also removes names configured only through the CLI. The provider verifies native convergence and preservation of unrelated configuration before saving. Synchronization and convergence are bounded by the RESTCONF timeout.

Use the data source to inspect an existing VE without taking ownership:

```hcl
data "fastiron_interface_ve" "transit" {
  ve_id = 3053
}

output "transit_enabled" {
  value = data.fastiron_interface_ve.transit.enabled
}
```

It returns `name`, `vlan_id`, `port_name` and native administrative `enabled` state. A missing VE is an error. The query does not change or save configuration, and administrative enablement does not establish link or routing readiness.

Destroy checks native configuration and refuses to remove a VE with addresses, protocol bindings, or other child settings. Remove those children first. It verifies that unrelated configuration is preserved and waits for both native and RESTCONF absence before saving. The VLAN resource independently guards against deletion while a VE still exists.

Failed operations retain observed resource state and `persistence_pending` so a later apply or destroy can retry reconciliation or saving. Refresh does not clear pending persistence, including when a failed deletion removed the running VE but did not save its removal.

VE administrative enable state is not currently supported by this resource. On tested FastIron `09.0.10kT213`, RESTCONF PUT and PATCH acknowledged and echoed `enabled` changes without changing native administrative configuration. Native `disable` remains an independently owned child and blocks resource deletion until removed.

Hardware validation on FastIron `09.0.10kT213` covered defaults, name changes and omission, CLI drift repair, native-only name removal, recreation after CLI deletion, import, forced replacement, child preservation and guarded deletion. Running and saved configuration matched exactly across reboot, followed by a no-change plan. Post-reboot updates and VE deletion preserved the parent VLAN. The data source verified default and CLI-configured name/administrative values, rejected a missing VE, and left running and saved configuration unchanged. Both workflows restored their exact original configuration.

Automated OpenTofu tests also cover acknowledged saves that fail to persist creation, updates or deletion, retention of pending state through refresh, and successful retries.
