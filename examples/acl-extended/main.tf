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

resource "fastiron_ip_access_list_extended" "web" {
  name = "WEB"

  rule {
    sequence         = 10
    action           = "permit"
    source           = "192.0.2.0/24"
    destination      = "198.51.100.10/32"
    protocol         = 6
    destination_port = "443"
  }

  rule {
    sequence = 20
    action   = "deny"
  }
}

resource "fastiron_ip_access_group" "web" {
  interface = var.interface
  direction = "in"
  acl       = fastiron_ip_access_list_extended.web.name
}
