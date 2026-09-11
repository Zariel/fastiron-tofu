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
  description = "PEM certificate authority for the switch HTTPS certificate."
}

variable "known_hosts" {
  type        = string
  description = "Trusted SSH known_hosts contents for the switch."
}

variable "radius_secret" {
  type      = string
  sensitive = true
}

variable "tacacs_secret" {
  type      = string
  sensitive = true
}

# Supply transport credentials through FASTIRON_USERNAME and FASTIRON_PASSWORD.
provider "fastiron" {
  host              = var.host
  allow_aaa_changes = true
  persistence_mode  = "after_each_write"

  restconf {
    ca_certificate = var.ca_certificate
  }
  ssh {
    known_hosts = var.known_hosts
  }
}

resource "fastiron_aaa_radius_server" "authentication" {
  address = "192.0.2.53"
  purpose = "authentication-only"
  secret  = var.radius_secret
}

resource "fastiron_aaa_tacacs_server" "administrators" {
  address = "192.0.2.54"
  secret  = var.tacacs_secret
}

data "fastiron_aaa_servers" "switch" {
  depends_on = [
    fastiron_aaa_radius_server.authentication,
    fastiron_aaa_tacacs_server.administrators,
  ]
}

output "servers" {
  value = data.fastiron_aaa_servers.switch.servers
}

variable "reader_password" {
  type      = string
  sensitive = true
}

resource "fastiron_aaa_user" "reader" {
  username  = "tofu-reader"
  privilege = 5
  password  = var.reader_password
}

data "fastiron_aaa_users" "switch" {
  depends_on = [fastiron_aaa_user.reader]
}

output "local_user_privileges" {
  value = data.fastiron_aaa_users.switch.users
}
