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

variable "lag_id" {
  type        = number
  description = "Unused positive aggregate identifier."
}

variable "members" {
  type        = set(string)
  description = "Compatible Ethernet ports without independent VLAN or routed configuration, such as ethernet 1/1/7 and ethernet 1/1/8."
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

resource "fastiron_lag" "storage" {
  lag_id  = var.lag_id
  name    = "storage"
  mode    = "dynamic"
  members = var.members
}

resource "fastiron_interface_lag" "storage" {
  lag_id    = fastiron_lag.storage.lag_id
  port_name = "Storage network"
  enabled   = true

  # Reapply interface policy when a mode change replaces the same numeric LAG ID.
  lifecycle {
    replace_triggered_by = [fastiron_lag.storage.id]
  }
}

data "fastiron_lag" "storage" {
  lag_id     = fastiron_lag.storage.lag_id
  depends_on = [fastiron_lag.storage]
}

data "fastiron_interface_lag" "storage" {
  lag_id     = fastiron_lag.storage.lag_id
  depends_on = [fastiron_interface_lag.storage]
}

output "aggregate" {
  value = data.fastiron_lag.storage
}

output "interface" {
  value = data.fastiron_interface_lag.storage
}
