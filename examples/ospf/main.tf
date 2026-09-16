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

variable "area_id" {
  type        = string
  description = "Canonical dotted OSPF area identifier, such as 0.0.0.53."
}

variable "interface" {
  type        = string
  description = "Existing default-VRF interface with IP configuration, such as ve 53."
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

resource "fastiron_router_ospf_area" "transit" {
  area_id = var.area_id
}

resource "fastiron_router_ospf_interface" "transit" {
  area_id   = fastiron_router_ospf_area.transit.area_id
  interface = var.interface
}

data "fastiron_ospf_area" "transit" {
  area_id    = fastiron_router_ospf_area.transit.area_id
  depends_on = [fastiron_router_ospf_interface.transit]
}

data "fastiron_ospf_areas" "configured" {
  depends_on = [fastiron_router_ospf_interface.transit]
}

output "area" {
  value = data.fastiron_ospf_area.transit
}

output "configured_areas" {
  value = data.fastiron_ospf_areas.configured.areas
}
