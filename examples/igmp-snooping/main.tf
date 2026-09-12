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

resource "fastiron_igmp_snooping" "global" {
  querier_mode = "active"
  version      = 3
}

resource "fastiron_vlan" "media" {
  vlan_id = var.vlan_id
  name    = "MEDIA"
}

resource "fastiron_vlan_igmp_snooping" "media" {
  vlan_id      = fastiron_vlan.media.vlan_id
  querier_mode = "passive"
  version      = 3
}
