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

variable "prefix" {
  type        = string
  description = "Canonical IPv4 destination network, such as 198.51.100.0/24."
}

variable "next_hop" {
  type        = string
  description = "IPv4 gateway address in the default VRF."
}

variable "distance" {
  type        = number
  default     = 1
  description = "Administrative distance from 1 to 255. Changing it replaces this route relationship."
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

resource "fastiron_ip_route" "network" {
  prefix   = var.prefix
  next_hop = var.next_hop
  distance = var.distance
}

data "fastiron_static_route" "network" {
  prefix     = fastiron_ip_route.network.prefix
  next_hop   = fastiron_ip_route.network.next_hop
  depends_on = [fastiron_ip_route.network]
}

data "fastiron_static_routes" "configured" {
  depends_on = [fastiron_ip_route.network]
}

output "route" {
  value = data.fastiron_static_route.network
}

output "configured_routes" {
  value = data.fastiron_static_routes.configured.routes
}
