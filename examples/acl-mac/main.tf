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
variable "interface" {
  type        = string
  description = "Canonical Ethernet, LAG or VLAN target to filter, such as ethernet 1/1/9."
}

# Supply transport credentials through FASTIRON_USERNAME and FASTIRON_PASSWORD.
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

resource "fastiron_mac_access_list" "hosts" {
  name = "HOSTS"

  rule {
    action    = "permit"
    source    = "02:00:00:00:00:01"
    ethertype = 2048
  }

  rule {
    action = "deny"
  }
}

resource "fastiron_mac_access_group" "hosts" {
  interface = var.interface
  acl       = fastiron_mac_access_list.hosts.name
}
