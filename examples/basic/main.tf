terraform {
  required_providers {
    fastiron = {
      source  = "zariel/fastiron"
      version = "0.0.1"
    }
  }
}

variable "host" {
  type = string
}

variable "ca_certificate" {
  type        = string
  description = "PEM certificate authority that signs the switch HTTPS certificate."
}

variable "known_hosts" {
  type        = string
  description = "Trusted SSH known_hosts contents for the switch."
}

variable "test_port" {
  type        = string
  description = "A disconnected Ethernet port, such as 1/1/2."
}

# Supply FASTIRON_USERNAME and FASTIRON_PASSWORD through the environment.
provider "fastiron" {
  host             = var.host
  persistence_mode = "after_each_write"

  restconf {
    ca_certificate = var.ca_certificate
  }

  ssh {
    known_hosts = var.known_hosts
  }
}

data "fastiron_capabilities" "switch" {}

resource "fastiron_vlan" "test" {
  vlan_id = 3053
  name    = "TOFU-TEST"
}

resource "fastiron_interface_ethernet" "test" {
  port      = var.test_port
  port_name = "TOFU-TEST"
  enabled   = true
}

output "firmware" {
  value = data.fastiron_capabilities.switch.firmware
}

# Each relationship owns only one membership; it preserves other VLANs on the port.
resource "fastiron_vlan_membership" "test" {
  vlan_id   = fastiron_vlan.test.vlan_id
  interface = fastiron_interface_ethernet.test.name
  tagging   = "tagged"
}
