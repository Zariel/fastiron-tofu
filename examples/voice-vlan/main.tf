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
variable "interface" { type = string }
variable "vlan_id" {
  type    = number
  default = 53
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

resource "fastiron_vlan" "voice" {
  vlan_id = var.vlan_id
  name    = "VOICE"
}

resource "fastiron_interface_voice_vlan" "phone" {
  interface = var.interface
  vlan_id   = fastiron_vlan.voice.vlan_id
}

data "fastiron_interface_voice_vlan" "phone" {
  interface  = var.interface
  depends_on = [fastiron_interface_voice_vlan.phone]
}

output "local_voice_vlan" {
  value = data.fastiron_interface_voice_vlan.phone.vlan_id
}
