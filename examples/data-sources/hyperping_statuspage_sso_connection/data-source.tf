# A SAML connection set up in the dashboard, looked up by name.
data "hyperping_statuspage_sso_connection" "okta" {
  name = "Okta"
}

resource "hyperping_statuspage" "partners" {
  name             = "Partner Status"
  hosted_subdomain = "acme-partners"

  settings = {
    name      = "Partner Status"
    languages = ["en"]

    authentication = {
      sso_connection_uuid = data.hyperping_statuspage_sso_connection.okta.id
    }
  }
}
