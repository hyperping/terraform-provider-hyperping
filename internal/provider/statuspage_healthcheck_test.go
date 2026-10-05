// Copyright (c) 2026 Hyperping
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	hyperping "github.com/hyperping/hyperping-go"
)

// Healthchecks on status pages: the read-only `type`, the plan-time checks on
// a healthcheck reference, and the healthcheck's `public_id`.

func hcNestedServiceObj(uuid types.String, showUptime, showResponseTimes types.Bool) types.Object {
	return types.ObjectValueMust(NestedServiceAttrTypes(), map[string]attr.Value{
		"id":                  types.StringUnknown(),
		"uuid":                uuid,
		"name":                types.MapNull(types.StringType),
		"is_group":            types.BoolNull(),
		"type":                types.StringUnknown(),
		"show_uptime":         showUptime,
		"show_response_times": showResponseTimes,
		"description":         types.MapNull(types.StringType),
	})
}

func hcServiceObj(uuid types.String, isGroup bool, showResponseTimes types.Bool, children []attr.Value) types.Object {
	nested := types.ListNull(types.ObjectType{AttrTypes: NestedServiceAttrTypes()})
	if children != nil {
		nested = types.ListValueMust(types.ObjectType{AttrTypes: NestedServiceAttrTypes()}, children)
	}
	var name types.Map
	if isGroup {
		name = types.MapValueMust(types.StringType, map[string]attr.Value{"en": types.StringValue("Jobs")})
	} else {
		name = types.MapNull(types.StringType)
	}
	return types.ObjectValueMust(ServiceAttrTypes(), map[string]attr.Value{
		"id":                  types.StringUnknown(),
		"uuid":                uuid,
		"name":                name,
		"is_group":            types.BoolValue(isGroup),
		"type":                types.StringUnknown(),
		"show_uptime":         types.BoolValue(true),
		"show_response_times": showResponseTimes,
		"description":         types.MapNull(types.StringType),
		"services":            nested,
	})
}

func hcSectionsList(services ...attr.Value) types.List {
	section := types.ObjectValueMust(SectionAttrTypes(), map[string]attr.Value{
		"name":     types.MapValueMust(types.StringType, map[string]attr.Value{"en": types.StringValue("Cron jobs")}),
		"is_split": types.BoolValue(true),
		"services": types.ListValueMust(types.ObjectType{AttrTypes: ServiceAttrTypes()}, services),
	})
	return types.ListValueMust(types.ObjectType{AttrTypes: SectionAttrTypes()}, []attr.Value{section})
}

func TestStatusPageServiceIssues(t *testing.T) {
	str := func(s string) *string { return &s }
	yes, no := true, false

	tests := []struct {
		name              string
		uuid              *string
		showResponseTimes *bool
		want              []string // attributes at fault
	}{
		{"unset uuid", nil, &yes, nil},
		{"monitor with response times", str("mon_abc"), &yes, nil},
		{"healthcheck with uptime only", str("hc_abc"), nil, nil},
		{"healthcheck with response times off", str("hc_abc"), &no, nil},
		{"healthcheck with response times on", str("hc_abc"), &yes, []string{"show_response_times"}},
		{"ping token", str("tok_abc"), nil, []string{"uuid"}},
		{"ping token with response times on", str("tok_abc"), &yes, []string{"uuid", "show_response_times"}},
		{"server", str("agt_abc"), &yes, nil},
		{"component", str("comp_abc"), &no, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := statusPageServiceIssues(tt.uuid, tt.showResponseTimes)
			var got []string
			for _, i := range issues {
				got = append(got, i.attribute)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("issues on %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateStatusPageSections(t *testing.T) {
	t.Run("healthchecks with uptime bars are valid", func(t *testing.T) {
		var d diag.Diagnostics
		validateStatusPageSections(hcSectionsList(
			hcServiceObj(types.StringValue("hc_a"), false, types.BoolNull(), nil),
			hcServiceObj(types.StringNull(), true, types.BoolNull(), []attr.Value{
				hcNestedServiceObj(types.StringValue("hc_b"), types.BoolValue(true), types.BoolValue(false)),
			}),
		), &d)
		if d.HasError() {
			t.Fatalf("unexpected errors: %v", d.Errors())
		}
	})

	t.Run("response times on a healthcheck point at the attribute", func(t *testing.T) {
		var d diag.Diagnostics
		validateStatusPageSections(hcSectionsList(
			hcServiceObj(types.StringValue("hc_a"), false, types.BoolValue(true), nil),
		), &d)
		if d.ErrorsCount() != 1 {
			t.Fatalf("want 1 error, got %v", d.Errors())
		}
		withPath, ok := d.Errors()[0].(diag.DiagnosticWithPath)
		if !ok {
			t.Fatalf("error has no attribute path: %v", d.Errors()[0])
		}
		if got := withPath.Path().String(); got != "sections[0].services[0].show_response_times" {
			t.Errorf("path = %s", got)
		}
	})

	t.Run("ping token in a group child", func(t *testing.T) {
		var d diag.Diagnostics
		validateStatusPageSections(hcSectionsList(
			hcServiceObj(types.StringNull(), true, types.BoolNull(), []attr.Value{
				hcNestedServiceObj(types.StringValue("mon_x"), types.BoolNull(), types.BoolNull()),
				hcNestedServiceObj(types.StringValue("tok_secret"), types.BoolNull(), types.BoolNull()),
			}),
		), &d)
		if d.ErrorsCount() != 1 {
			t.Fatalf("want 1 error, got %v", d.Errors())
		}
		withPath := d.Errors()[0].(diag.DiagnosticWithPath)
		if got := withPath.Path().String(); got != "sections[0].services[0].services[1].uuid" {
			t.Errorf("path = %s", got)
		}
		if !strings.Contains(d.Errors()[0].Detail(), "public_id") {
			t.Errorf("detail should point to public_id: %s", d.Errors()[0].Detail())
		}
	})

	t.Run("unknown values wait for apply", func(t *testing.T) {
		var d diag.Diagnostics
		validateStatusPageSections(hcSectionsList(
			hcServiceObj(types.StringUnknown(), false, types.BoolValue(true), nil),
		), &d)
		if d.HasError() {
			t.Fatalf("unexpected errors: %v", d.Errors())
		}
		validateStatusPageSections(types.ListUnknown(types.ObjectType{AttrTypes: SectionAttrTypes()}), &d)
		if d.HasError() {
			t.Fatalf("unexpected errors: %v", d.Errors())
		}
	})
}

// Values unknown at plan time (public_id of a healthcheck created in the same
// apply) are checked when the request is built.
func TestMapTFToSections_HealthcheckChecksAtApply(t *testing.T) {
	var d diag.Diagnostics
	mapTFToSections(hcSectionsList(
		hcServiceObj(types.StringValue("hc_a"), false, types.BoolValue(true), nil),
		hcServiceObj(types.StringNull(), true, types.BoolNull(), []attr.Value{
			hcNestedServiceObj(types.StringValue("tok_secret"), types.BoolNull(), types.BoolNull()),
		}),
	), &d)
	if d.ErrorsCount() != 2 {
		t.Fatalf("want 2 errors, got %v", d.Errors())
	}
}

func TestMapTFToNestedServices_SendsBooleans(t *testing.T) {
	var d diag.Diagnostics
	list := types.ListValueMust(types.ObjectType{AttrTypes: NestedServiceAttrTypes()}, []attr.Value{
		hcNestedServiceObj(types.StringValue("hc_b"), types.BoolValue(false), types.BoolValue(false)),
		hcNestedServiceObj(types.StringValue("mon_c"), types.BoolUnknown(), types.BoolNull()),
	})
	got := mapTFToNestedServices(list, &d)
	if d.HasError() {
		t.Fatalf("unexpected errors: %v", d.Errors())
	}
	if got[0].ShowUptime == nil || *got[0].ShowUptime {
		t.Errorf("show_uptime=false must be sent for a group child, got %v", got[0].ShowUptime)
	}
	if got[0].ShowResponseTimes == nil || *got[0].ShowResponseTimes {
		t.Errorf("show_response_times=false must be sent, got %v", got[0].ShowResponseTimes)
	}
	if got[1].ShowUptime != nil || got[1].ShowResponseTimes != nil {
		t.Errorf("unset or unknown booleans must not be sent, got %v %v", got[1].ShowUptime, got[1].ShowResponseTimes)
	}
	body, _ := json.Marshal(got[1])
	if strings.Contains(string(body), "show_") {
		t.Errorf("unexpected booleans in %s", body)
	}
}

// What GET /v2/statuspages/{uuid} returns for a page built in the dashboard,
// healthchecks included (prompt 01 contract).
const statusPageWithHealthchecksJSON = `{
  "uuid": "sp_x", "name": "OXG", "hostedsubdomain": "oxg.hyperping.app", "url": "oxg.hyperping.app",
  "settings": {"name": "OXG", "languages": ["en"], "default_language": "en"},
  "sections": [{
    "name": {"en": "Cron jobs", "fr": "", "de": "", "ru": ""},
    "is_split": true,
    "services": [
      {"id": "hc_a", "uuid": "hc_a", "name": {"en": "Backup"}, "is_group": false, "type": "healthcheck",
       "show_uptime": true, "show_response_times": false},
      {"name": {"en": "Nightly"}, "is_group": true, "show_uptime": false, "show_response_times": false,
       "services": [
         {"id": "hc_b", "uuid": "hc_b", "name": {"en": "Sync"}, "type": "healthcheck", "show_uptime": true, "show_response_times": false},
         {"id": 117122, "uuid": "mon_c", "name": {"en": "API"}, "type": "monitor", "show_uptime": true, "show_response_times": true}
       ]},
      {"id": "agt_d", "uuid": "agt_d", "name": {"en": "DB host"}, "is_group": false, "type": "server",
       "show_uptime": true, "show_response_times": false}
    ]
  }]
}`

func TestMapSections_ReadsServiceType(t *testing.T) {
	var sp hyperping.StatusPage
	if err := json.Unmarshal([]byte(statusPageWithHealthchecksJSON), &sp); err != nil {
		t.Fatal(err)
	}
	var d diag.Diagnostics
	fields := MapStatusPageCommonFieldsWithFilter(&sp, []string{"en"}, &d)
	if d.HasError() {
		t.Fatalf("unexpected errors: %v", d.Errors())
	}

	services := fields.Sections.Elements()[0].(types.Object).Attributes()["services"].(types.List).Elements()
	typeOf := func(o attr.Value) types.String { return o.(types.Object).Attributes()["type"].(types.String) }

	if got := typeOf(services[0]); got.ValueString() != "healthcheck" {
		t.Errorf("services[0].type = %s", got)
	}
	if got := typeOf(services[1]); !got.IsNull() {
		t.Errorf("a group has no type, got %s", got)
	}
	if got := typeOf(services[2]); got.ValueString() != "server" {
		t.Errorf("services[2].type = %s", got)
	}
	children := services[1].(types.Object).Attributes()["services"].(types.List).Elements()
	if got := typeOf(children[0]); got.ValueString() != "healthcheck" {
		t.Errorf("child[0].type = %s", got)
	}
	if got := typeOf(children[1]); got.ValueString() != "monitor" {
		t.Errorf("child[1].type = %s", got)
	}
	if got := children[1].(types.Object).Attributes()["id"].(types.String).ValueString(); got != "117122" {
		t.Errorf("integer child id = %s", got)
	}
}

func TestMapHealthcheckCommonFields_PublicID(t *testing.T) {
	var hc hyperping.Healthcheck
	if err := json.Unmarshal([]byte(`{"uuid":"tok_secret","publicUuid":"hc_public","name":"Backup"}`), &hc); err != nil {
		t.Fatal(err)
	}
	f := MapHealthcheckCommonFields(&hc)
	if f.ID.ValueString() != "tok_secret" || f.PublicID.ValueString() != "hc_public" {
		t.Errorf("id=%s public_id=%s", f.ID, f.PublicID)
	}

	for _, body := range []string{`{"uuid":"tok_a"}`, `{"uuid":"tok_a","publicUuid":null}`, `{"uuid":"tok_a","publicUuid":""}`} {
		var legacy hyperping.Healthcheck
		if err := json.Unmarshal([]byte(body), &legacy); err != nil {
			t.Fatal(err)
		}
		if got := MapHealthcheckCommonFields(&legacy).PublicID; !got.IsNull() {
			t.Errorf("%s: public_id = %s, want null", body, got)
		}
	}

	if !MapHealthcheckCommonFields(nil).PublicID.IsNull() {
		t.Error("nil healthcheck must map public_id to null")
	}
}

// TestMapHealthcheckCommonFields_TimezoneFromTz is the regression test for
// "timezone was Europe/Berlin, but now null" on a cron healthcheck: GET and
// PUT /v2/healthchecks return the timezone as "tz", POST as "timezone".
func TestMapHealthcheckCommonFields_TimezoneFromTz(t *testing.T) {
	for _, hc := range []hyperping.Healthcheck{
		{UUID: "tok_a", Cron: "0 3 * * *", Tz: "Europe/Berlin"},
		{UUID: "tok_a", Cron: "0 3 * * *", Timezone: "Europe/Berlin"},
	} {
		if got := MapHealthcheckCommonFields(&hc).Timezone; got.ValueString() != "Europe/Berlin" {
			t.Errorf("%+v: timezone = %s, want Europe/Berlin", hc, got)
		}
	}

	var decoded hyperping.Healthcheck
	if err := json.Unmarshal([]byte(`{"uuid":"tok_a","cron":"0 3 * * *","tz":"Europe/Berlin"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if got := MapHealthcheckCommonFields(&decoded).Timezone; got.ValueString() != "Europe/Berlin" {
		t.Errorf("GET shape: timezone = %s, want Europe/Berlin", got)
	}

	// The API stores a timezone (UTC by default) for period-based
	// healthchecks too; it only applies to a cron schedule, so it stays null.
	for _, hc := range []hyperping.Healthcheck{
		{UUID: "tok_b"},
		{UUID: "tok_b", Tz: "UTC", PeriodValue: func() *int { v := 5; return &v }(), PeriodType: "minutes"},
	} {
		if got := MapHealthcheckCommonFields(&hc).Timezone; !got.IsNull() {
			t.Errorf("period healthcheck %+v: timezone = %s, want null", hc, got)
		}
	}
}
