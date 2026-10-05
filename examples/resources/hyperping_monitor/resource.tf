# Basic HTTP monitor - just URL and name
resource "hyperping_monitor" "basic" {
  name     = "My Website"
  url      = "https://example.com"
  protocol = "http"
}

# Full-featured HTTP monitor with all options
resource "hyperping_monitor" "api_health" {
  name                 = "API Health Check"
  url                  = "https://api.example.com/health"
  protocol             = "http"
  http_method          = "POST"
  check_frequency      = 300 # 5 minutes
  expected_status_code = "201"
  follow_redirects     = false

  regions = ["london", "virginia", "singapore"]

  request_headers = [
    {
      name  = "Content-Type"
      value = "application/json"
    },
    {
      name  = "X-Auth-Token"
      value = "${var.api_token}"
    }
  ]

  request_body = jsonencode({
    check = "health"
  })
}

# Monitor with pause capability
resource "hyperping_monitor" "maintenance" {
  name     = "Service Under Maintenance"
  url      = "https://maintenance.example.com"
  protocol = "http"
  paused   = true
}

# Monitor checked over IPv6 only (http, port and icmp)
resource "hyperping_monitor" "ipv6" {
  name       = "API over IPv6"
  url        = "https://api.example.com/health"
  protocol   = "http"
  ip_version = 6
}

# Monitor with TLS certificate and domain expiry alerts
resource "hyperping_monitor" "expiry_alerts" {
  name     = "Customer Portal"
  url      = "https://portal.example.com"
  protocol = "http"

  # First certificate expiry alert 30 days ahead, then reminders at 15, 7, 3 and 1 days
  ssl_alert_days = 30
  ssl_reminders  = true

  # Alert when the site starts serving a different certificate
  ssl_notify_on_change = true

  # Alert 30 days before the domain registration expires (-1 disables)
  domain_alert_days = 30
}

output "portal_certificate_days_left" {
  value = hyperping_monitor.expiry_alerts.ssl_expiration
}

output "portal_domain_days_left" {
  value = hyperping_monitor.expiry_alerts.domain_expiration
}
