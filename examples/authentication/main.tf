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
variable "port" {
  type        = string
  description = "Ethernet stack/slot/port to use for authentication."
}
variable "auth_vlan" {
  type        = number
  description = "VLAN ID to use as the authentication default VLAN."
}
variable "radius_address" { type = string }
variable "radius_secret" {
  type      = string
  sensitive = true
}

# Supply transport credentials through FASTIRON_USERNAME and FASTIRON_PASSWORD.
provider "fastiron" {
  host              = var.host
  allow_aaa_changes = true
  restconf {
    ca_certificate = var.ca_certificate
  }
  ssh {
    known_hosts = var.known_hosts
  }
}

resource "fastiron_vlan" "authentication" {
  vlan_id = var.auth_vlan
  name    = "AUTHENTICATION"
}

resource "fastiron_aaa_radius_server" "authentication" {
  address = var.radius_address
  purpose = "authentication-only"
  secret  = var.radius_secret
}

resource "fastiron_aaa" "policy" {
  login_methods = ["local"]
  dot1x_default = "radius"

  depends_on = [fastiron_aaa_radius_server.authentication]
}

resource "fastiron_authentication" "switch" {
  auth_default_vlan          = fastiron_vlan.authentication.vlan_id
  dot1x_enabled              = true
  mac_authentication_enabled = true
}

resource "fastiron_authentication_interface" "access" {
  interface                  = "ethernet ${var.port}"
  dot1x_enabled              = true
  mac_authentication_enabled = true
  port_control               = "auto"

  depends_on = [fastiron_authentication.switch, fastiron_aaa.policy]
}
