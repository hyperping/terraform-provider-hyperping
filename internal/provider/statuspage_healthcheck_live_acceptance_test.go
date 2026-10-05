// Copyright (c) 2026 Hyperping
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	hyperping "github.com/hyperping/hyperping-go"
)

// Healthchecks on a status page, against a real Hyperping API (not a mock):
// the API resolves hc_ ids and returns the service type, which a mock would
// only restate. Opt-in, since it creates and deletes real resources:
//
//	TF_ACC=1 HYPERPING_API_KEY=sk_… HYPERPING_ACC_BASE_URL=http://localhost:5500 \
//	  go test ./internal/provider -run TestAccStatusPageHealthchecksLive -v
func testAccLiveAPI(t *testing.T) (baseURL, apiKey string) {
	t.Helper()
	baseURL = os.Getenv("HYPERPING_ACC_BASE_URL")
	apiKey = os.Getenv("HYPERPING_API_KEY")
	if baseURL == "" || apiKey == "" {
		t.Skip("HYPERPING_ACC_BASE_URL and HYPERPING_API_KEY must be set for live acceptance tests")
	}
	return baseURL, apiKey
}

func TestAccStatusPageHealthchecksLive(t *testing.T) {
	baseURL, apiKey := testAccLiveAPI(t)
	suffix := strings.ToLower(acctest.RandString(8))
	sp := "hyperping_statuspage.oxg"

	tfresource.Test(t, tfresource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckStatusPageGone(baseURL, apiKey),
		Steps: []tfresource.TestStep{
			// A healthcheck directly in a section and another one in a group.
			// The framework fails the step if the plan after apply is not empty.
			{
				Config: testAccStatusPageHealthchecksLiveConfig(baseURL, apiKey, suffix, true, ""),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestMatchResourceAttr("hyperping_healthcheck.backup", "public_id", regexp.MustCompile(`^hc_`)),
					tfresource.TestMatchResourceAttr("hyperping_healthcheck.backup", "id", regexp.MustCompile(`^tok_`)),
					tfresource.TestCheckResourceAttrPair(sp, "sections.0.services.0.uuid", "hyperping_healthcheck.backup", "public_id"),
					tfresource.TestCheckResourceAttr(sp, "sections.0.services.0.type", "healthcheck"),
					tfresource.TestCheckResourceAttr(sp, "sections.0.services.0.show_uptime", "true"),
					tfresource.TestCheckResourceAttr(sp, "sections.0.services.0.show_response_times", "false"),
					tfresource.TestCheckResourceAttr(sp, "sections.0.services.1.is_group", "true"),
					tfresource.TestCheckNoResourceAttr(sp, "sections.0.services.1.type"),
					tfresource.TestCheckResourceAttrPair(sp, "sections.0.services.1.services.0.uuid", "hyperping_healthcheck.sync", "public_id"),
					tfresource.TestCheckResourceAttr(sp, "sections.0.services.1.services.0.type", "healthcheck"),
					tfresource.TestCheckResourceAttr(sp, "sections.0.services.1.services.0.show_uptime", "true"),
					tfresource.TestCheckResourceAttr(sp, "sections.0.services.1.services.0.show_response_times", "false"),
				),
			},
			// Import: same state as the one built by apply.
			{
				ResourceName:      sp,
				ImportState:       true,
				ImportStateVerify: true,
			},
			// show_uptime turned off on the nested healthcheck: sent and kept.
			{
				Config: testAccStatusPageHealthchecksLiveConfig(baseURL, apiKey, suffix, false, ""),
				Check: tfresource.ComposeAggregateTestCheckFunc(
					tfresource.TestCheckResourceAttr(sp, "sections.0.services.1.services.0.show_uptime", "false"),
					tfresource.TestCheckResourceAttr(sp, "sections.0.services.1.services.0.type", "healthcheck"),
				),
			},
			// Refused at plan time: response times on a healthcheck…
			{
				Config:      testAccStatusPageHealthchecksLiveConfig(baseURL, apiKey, suffix, false, "show_response_times = true"),
				ExpectError: regexp.MustCompile(`show_response_times is not available for a healthcheck`),
			},
			// …and the ping token instead of the public id.
			{
				Config:      testAccStatusPageHealthchecksLiveConfig(baseURL, apiKey, suffix, false, "token"),
				ExpectError: regexp.MustCompile(`Healthcheck ping token used as a status page service`),
			},
		},
	})
}

// variant: "" (valid), "show_response_times = true" or "token" (uuid = id).
func testAccStatusPageHealthchecksLiveConfig(baseURL, apiKey, suffix string, nestedUptime bool, variant string) string {
	firstRef := "hyperping_healthcheck.backup.public_id"
	firstExtra := ""
	switch variant {
	case "token":
		firstRef = "hyperping_healthcheck.backup.id"
	case "":
	default:
		firstExtra = variant
	}
	return fmt.Sprintf(`
provider "hyperping" {
  api_key  = %[1]q
  base_url = %[2]q
}

resource "hyperping_healthcheck" "backup" {
  name               = "tfacc-backup-%[3]s"
  period_value       = 1
  period_type        = "days"
  grace_period_value = 1
  grace_period_type  = "hours"
}

resource "hyperping_healthcheck" "sync" {
  name               = "tfacc-sync-%[3]s"
  period_value       = 1
  period_type        = "hours"
  grace_period_value = 15
  grace_period_type  = "minutes"
}

resource "hyperping_statuspage" "oxg" {
  name             = "tfacc healthchecks %[3]s"
  hosted_subdomain = "tfacc-hc-%[3]s"

  settings = {
    name      = "tfacc healthchecks %[3]s"
    languages = ["en"]
  }

  sections = [{
    name     = { en = "Cron jobs" }
    is_split = true
    services = [
      {
        uuid        = %[4]s
        show_uptime = true
        %[5]s
      },
      {
        is_group = true
        name     = { en = "Sync" }
        services = [{
          uuid        = hyperping_healthcheck.sync.public_id
          show_uptime = %[6]t
        }]
      },
    ]
  }]
}
`, apiKey, baseURL, suffix, firstRef, firstExtra, nestedUptime)
}

func testAccCheckStatusPageGone(baseURL, apiKey string) func(*terraform.State) error {
	return func(s *terraform.State) error {
		c := hyperping.NewClient(apiKey, hyperping.WithBaseURL(baseURL))
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "hyperping_statuspage" {
				continue
			}
			_, err := c.GetStatusPage(context.Background(), rs.Primary.ID)
			if err == nil {
				return fmt.Errorf("status page %s still exists", rs.Primary.ID)
			}
			if !hyperping.IsNotFound(err) {
				return err
			}
		}
		return nil
	}
}
