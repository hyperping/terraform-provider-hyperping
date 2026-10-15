# Import an OpenID Connect connection by its ID. The client secret is never
# returned by the API: keep it in the configuration, the next apply sends it once.
terraform import hyperping_statuspage_sso_connection.auth0 sso_abc123xyz
