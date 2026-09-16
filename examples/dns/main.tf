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

variable "server_addresses" {
  type        = set(string)
  description = "Canonical DNS server IP addresses to manage. Other configured servers remain independently owned."
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

resource "fastiron_ip_dns_server" "managed" {
  for_each = var.server_addresses
  address  = each.value
}

data "fastiron_ip_dns_servers" "configured" {
  depends_on = [fastiron_ip_dns_server.managed]
}

output "configured_servers" {
  value = data.fastiron_ip_dns_servers.configured.addresses
}
