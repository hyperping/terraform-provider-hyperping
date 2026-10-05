# Basic status page example
resource "hyperping_statuspage" "basic" {
  name             = "My Status Page"
  hosted_subdomain = "status"

  settings = {
    name      = "My Status Page"
    languages = ["en"]
  }
}

# Status page with monitors, healthchecks and groups
resource "hyperping_statuspage" "production" {
  name             = "Production Status"
  hosted_subdomain = "prod-status"

  # Optional: Use custom domain instead of hosted subdomain
  # hostname = "status.example.com"

  # Optional: Password protect the page
  # password = "secret"

  settings = {
    name             = "Production Status"
    website          = "https://example.com"
    languages        = ["en", "fr"]
    default_language = "en"

    # Theme and branding
    theme        = "dark"    # Options: system, light, dark
    font         = "Inter"   # Options: Inter, Roboto, Poppins, Lato, etc.
    accent_color = "#0066cc" # Brand color (hex)

    # Multi-language description
    description = "Production system status and uptime"

    # Subscription settings
    subscribe = {
      enabled = true
      email   = true
      sms     = true
      slack   = false # Configured via Hyperping OAuth
      teams   = true
    }

    # Authentication settings
    authentication = {
      password_protection = false
      google_sso          = true
      allowed_domains     = ["example.com", "partner.com"]
    }
  }

  # Status page sections with monitors
  sections = [
    {
      name = {
        en = "Core API Services"
        fr = "Services API principaux"
      }
      is_split = true # Show individual service status
      services = [
        {
          uuid = hyperping_monitor.api.id
          name = {
            en = "Main API"
          }
          show_uptime         = true
          show_response_times = true
        },
        {
          uuid = hyperping_monitor.auth.id
          name = {
            en = "Authentication API"
          }
          show_uptime         = true
          show_response_times = false
        }
      ]
    },
    {
      name = {
        en = "Infrastructure"
        fr = "Infrastructure"
      }
      is_split = false # Show aggregated status
      services = [
        {
          uuid     = hyperping_monitor.database.id
          is_group = true
          name = {
            en = "Database Cluster"
          }
          services = [
            {
              uuid = hyperping_monitor.db_primary.id
              name = {
                en = "Primary DB"
              }
            },
            {
              uuid = hyperping_monitor.db_replica.id
              name = {
                en = "Replica DB"
              }
            }
          ]
        }
      ]
    },
    {
      name = {
        en = "Scheduled jobs"
        fr = "Tâches planifiées"
      }
      is_split = true
      services = [
        # A healthcheck is referenced by its public id (hc_…), never by `id`,
        # which is the secret token of its ping URL. Uptime bars are supported;
        # response times are not (leave show_response_times unset or false).
        {
          uuid        = hyperping_healthcheck.backup.public_id
          name        = { en = "Nightly backup" }
          show_uptime = true
        },
        # A group mixing healthchecks and monitors
        {
          is_group = true
          name = {
            en = "Data pipelines"
            fr = "Pipelines de données"
          }
          services = [
            {
              uuid        = hyperping_healthcheck.etl.public_id
              name        = { en = "ETL" }
              show_uptime = true
            },
            {
              uuid        = hyperping_healthcheck.sync.public_id
              name        = { en = "CRM sync" }
              show_uptime = false
            },
            {
              uuid = hyperping_monitor.api.id
              name = {
                en = "Ingestion API"
              }
            }
          ]
        }
      ]
    }
  ]
}

resource "hyperping_healthcheck" "backup" {
  name               = "Nightly backup"
  cron               = "0 3 * * *"
  timezone           = "Europe/Berlin"
  grace_period_value = 1
  grace_period_type  = "hours"
}

resource "hyperping_healthcheck" "etl" {
  name               = "ETL"
  period_value       = 6
  period_type        = "hours"
  grace_period_value = 30
  grace_period_type  = "minutes"
}

resource "hyperping_healthcheck" "sync" {
  name               = "CRM sync"
  period_value       = 15
  period_type        = "minutes"
  grace_period_value = 5
  grace_period_type  = "minutes"
}

# Example monitors (referenced in status page)
resource "hyperping_monitor" "api" {
  name                 = "Production API"
  url                  = "https://api.example.com/health"
  protocol             = "http"
  http_method          = "GET"
  check_frequency      = 60
  expected_status_code = "2xx"
}

resource "hyperping_monitor" "auth" {
  name            = "Auth Service"
  url             = "https://auth.example.com/health"
  protocol        = "http"
  http_method     = "GET"
  check_frequency = 60
}

resource "hyperping_monitor" "database" {
  name            = "Database Health"
  url             = "https://db.example.com/health"
  protocol        = "http"
  http_method     = "GET"
  check_frequency = 300
}

resource "hyperping_monitor" "db_primary" {
  name            = "DB Primary"
  url             = "https://db-primary.example.com"
  protocol        = "port"
  port            = 5432
  check_frequency = 60
}

resource "hyperping_monitor" "db_replica" {
  name            = "DB Replica"
  url             = "https://db-replica.example.com"
  protocol        = "port"
  port            = 5432
  check_frequency = 60
}

# Output the status page URL
# Each service reports its type, read from the API:
# monitor, healthcheck, server or component (null for a group header).
output "scheduled_jobs_types" {
  value = [for s in hyperping_statuspage.production.sections[2].services : s.type]
}

output "status_page_url" {
  value       = hyperping_statuspage.production.url
  description = "Public URL of the status page"
}

output "status_page_id" {
  value       = hyperping_statuspage.production.id
  description = "UUID of the status page for use with subscribers"
}
