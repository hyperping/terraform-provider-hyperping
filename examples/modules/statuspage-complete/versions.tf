# Status Page Complete Module - Provider Requirements

terraform {
  required_version = ">= 1.11"

  required_providers {
    hyperping = {
      source  = "hyperping/hyperping"
      version = "~> 2.1"
    }
  }
}
