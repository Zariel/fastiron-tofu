terraform {
  required_providers {
    fastiron = {
      source  = "zariel/fastiron"
      version = "0.0.1"
    }
  }
}

variable "host" { type = string }
variable "ca_certificate" { type = string }
variable "known_hosts" { type = string }
variable "vlan_id" {
  type    = number
  default = 4095
}

# Supply credentials through FASTIRON_USERNAME and FASTIRON_PASSWORD.
provider "fastiron" {
  host      = var.host
  transport = "restconf"
  restconf {
    ca_certificate = var.ca_certificate
  }
  ssh {
    known_hosts = var.known_hosts
  }
}

# Use an unused ID. Destroy resets the global default to VLAN 1.
resource "fastiron_default_vlan" "switch" {
  vlan_id = var.vlan_id
}

output "default_vlan" { value = fastiron_default_vlan.switch.vlan_id }
