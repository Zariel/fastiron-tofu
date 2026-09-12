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

resource "fastiron_ipv6_access_list" "dns" {
  name = "IPV6-DNS"

  rule {
    sequence         = 10
    action           = "permit"
    source           = "2001:db8:1::/64"
    destination      = "2001:db8:2::53/128"
    protocol         = 17
    destination_port = "53"
  }

  rule {
    sequence = 20
    action   = "deny"
    log      = true
  }
}

resource "fastiron_ipv6_access_group" "dns" {
  interface = var.interface
  direction = "in"
  acl       = fastiron_ipv6_access_list.dns.name
}
