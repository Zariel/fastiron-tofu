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
  default = 1
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

data "fastiron_vlans" "switch" {}
data "fastiron_vlan" "selected" { vlan_id = var.vlan_id }

output "vlans" { value = data.fastiron_vlans.switch.vlans }
output "selected" { value = data.fastiron_vlan.selected }
