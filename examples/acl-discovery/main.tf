terraform {
  required_providers {
    fastiron = {
      source  = "zariel/fastiron"
      version = "0.0.1"
    }
  }
}

variable "host" { type = string }
variable "known_hosts" { type = string }
variable "acl_kind" {
  type        = string
  description = "ipv4_standard, ipv4_extended, ipv6 or mac."
}
variable "acl_name" { type = string }

# Supply credentials through FASTIRON_USERNAME and FASTIRON_PASSWORD.
provider "fastiron" {
  host      = var.host
  transport = "ssh"
  ssh {
    known_hosts = var.known_hosts
  }
}

data "fastiron_acls" "switch" {}

data "fastiron_acl" "selected" {
  kind = var.acl_kind
  name = var.acl_name
}

output "inventory" { value = data.fastiron_acls.switch.acls }
output "rules" { value = data.fastiron_acl.selected.rules }
