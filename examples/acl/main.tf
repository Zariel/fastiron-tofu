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
  description = "Canonical Ethernet or LAG interface to filter, such as ethernet 1/1/9."
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

resource "fastiron_ip_access_list_standard" "sources" {
  name = "90"

  rule {
    sequence = 10
    action   = "permit"
    source   = "192.0.2.0/24"
  }

  rule {
    sequence = 20
    action   = "deny"
  }
}

resource "fastiron_ip_access_group" "sources" {
  interface = var.interface
  direction = "in"
  acl       = fastiron_ip_access_list_standard.sources.name
}
