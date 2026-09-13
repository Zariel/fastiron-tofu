terraform {
  required_providers {
    fastiron = { source = "zariel/fastiron" }
  }
}

variable "switch_host" {
  type = string
}

provider "fastiron" {
  host = var.switch_host
}

resource "fastiron_interface_protected_port" "guest" {
  interface = "ethernet 1/1/12"
  enabled   = true
}

data "fastiron_interface_protected_port" "guest" {
  interface = fastiron_interface_protected_port.guest.interface
}

output "protected" {
  value = data.fastiron_interface_protected_port.guest.enabled
}
