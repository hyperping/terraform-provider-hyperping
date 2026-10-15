# OpenID Connect connection for a private status page (Auth0 here; any
# OpenID Connect identity provider works: Okta, Microsoft Entra ID, Keycloak…).
variable "auth0_client_id" {
  type = string
}

variable "auth0_client_secret" {
  type      = string
  sensitive = true
}

resource "hyperping_statuspage_sso_connection" "auth0" {
  name          = "Auth0"
  issuer        = "https://acme.eu.auth0.com/"
  client_id     = var.auth0_client_id
  client_secret = var.auth0_client_secret

  # Only accounts with these email domains can sign in (omit for any).
  allowed_domains = ["acme.com"]
}

# Visitors of this page sign in through Auth0.
resource "hyperping_statuspage" "internal" {
  name             = "Internal Status"
  hosted_subdomain = "acme-internal"

  settings = {
    name      = "Internal Status"
    languages = ["en"]

    authentication = {
      sso_connection_uuid = hyperping_statuspage_sso_connection.auth0.id
    }
  }
}

# Register this URL as an allowed callback URL of the Auth0 application.
output "auth0_callback_url" {
  value = hyperping_statuspage_sso_connection.auth0.redirect_uri
}
