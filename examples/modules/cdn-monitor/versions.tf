# CDN Monitor Module - Provider Requirements
#
# This module requires the Hyperping provider.
# Provider configuration should be done in the root module.

terraform {
  required_version = ">= 1.11"

  required_providers {
    hyperping = {
      source  = "hyperping/hyperping"
      version = "~> 2.1"
    }
  }
}
