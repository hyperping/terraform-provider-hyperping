// Copyright (c) 2026 Develeap
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/develeap/terraform-provider-hyperping/internal/client"
)

// Tests for the SSL/domain expiry alert settings on hyperping_monitor:
// ssl_alert_days, ssl_reminders, ssl_notify_on_change, domain_alert_days (Optional+Computed)
// and domain_expiration (Computed).

var expiryWritableAttrs = []string{"ssl_alert_days", "ssl_reminders", "ssl_notify_on_change", "domain_alert_days"}

var expiryAllAttrs = []string{"ssl_expiration", "ssl_alert_days", "ssl_reminders", "ssl_notify_on_change", "domain_alert_days", "domain_expiration"}

func monitorResourceSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	(&MonitorResource{}).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func TestMonitorResource_Schema_ExpiryAlertAttributes(t *testing.T) {
	s := monitorResourceSchema(t)

	for _, name := range expiryWritableAttrs {
		attr, ok := s.Attributes[name]
		if !ok {
			t.Errorf("schema missing %q", name)
			continue
		}
		if !attr.IsOptional() || !attr.IsComputed() || attr.IsRequired() {
			t.Errorf("%q must be Optional+Computed (optional=%v computed=%v required=%v)",
				name, attr.IsOptional(), attr.IsComputed(), attr.IsRequired())
		}
		if attr.GetMarkdownDescription() == "" {
			t.Errorf("%q has no MarkdownDescription", name)
		}
	}

	// No hardcoded defaults: the server's default must be read back instead.
	if a, ok := s.Attributes["ssl_alert_days"].(schema.Int64Attribute); !ok || a.Default != nil {
		t.Error("ssl_alert_days must be an Int64Attribute without a provider-side default")
	}
	if a, ok := s.Attributes["domain_alert_days"].(schema.Int64Attribute); !ok || a.Default != nil {
		t.Error("domain_alert_days must be an Int64Attribute without a provider-side default")
	}
	if a, ok := s.Attributes["ssl_reminders"].(schema.BoolAttribute); !ok || a.Default != nil {
		t.Error("ssl_reminders must be a BoolAttribute without a provider-side default")
	}
	if a, ok := s.Attributes["ssl_notify_on_change"].(schema.BoolAttribute); !ok || a.Default != nil {
		t.Error("ssl_notify_on_change must be a BoolAttribute without a provider-side default")
	}

	for _, name := range []string{"domain_expiration", "ssl_expiration"} {
		attr, ok := s.Attributes[name]
		if !ok {
			t.Errorf("schema missing %q", name)
			continue
		}
		if !attr.IsComputed() || attr.IsOptional() || attr.IsRequired() {
			t.Errorf("%q must be Computed only", name)
		}
	}

	if got := s.Attributes["ssl_expiration"].GetMarkdownDescription(); got != "Whole days until the TLS certificate expires (rounded down)." {
		t.Errorf("unexpected ssl_expiration description: %q", got)
	}
}

func runInt64Validators(validators []validator.Int64, v int64) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, val := range validators {
		resp := &validator.Int64Response{}
		val.ValidateInt64(context.Background(), validator.Int64Request{
			Path:        path.Root("attr"),
			ConfigValue: types.Int64Value(v),
		}, resp)
		diags.Append(resp.Diagnostics...)
	}
	return diags
}

func TestMonitorResource_Schema_ExpiryAlertValidators(t *testing.T) {
	s := monitorResourceSchema(t)

	tests := []struct {
		attr    string
		valid   []int64
		invalid []int64
	}{
		{
			attr:    "ssl_alert_days",
			valid:   []int64{-1, 1, 3, 7, 15, 30, 60, 90},
			invalid: []int64{0, 2, 5, 14, 45, 91, 365, -2},
		},
		{
			attr:    "domain_alert_days",
			valid:   []int64{-1, 7, 14, 30, 60, 90},
			invalid: []int64{0, 1, 3, 15, 45, 120, -7},
		},
	}

	for _, tt := range tests {
		attr, ok := s.Attributes[tt.attr].(schema.Int64Attribute)
		if !ok {
			t.Fatalf("%q is not an Int64Attribute", tt.attr)
		}
		for _, v := range tt.valid {
			if diags := runInt64Validators(attr.Validators, v); diags.HasError() {
				t.Errorf("%s=%d should be valid, got %v", tt.attr, v, diags)
			}
		}
		for _, v := range tt.invalid {
			if diags := runInt64Validators(attr.Validators, v); !diags.HasError() {
				t.Errorf("%s=%d should be rejected", tt.attr, v)
			}
		}
	}
}

func TestMonitorResource_mapMonitorToModel_ExpiryAlerts(t *testing.T) {
	r := &MonitorResource{}

	t.Run("values mapped, including -1 and false", func(t *testing.T) {
		monitor := &client.Monitor{
			UUID:              "mon-exp",
			Name:              "Expiry",
			URL:               "https://example.com",
			Protocol:          "http",
			SSLExpiration:     intPtrForTest(41),
			SSLAlertDays:      intPtrForTest(30),
			SSLReminders:      boolPtrForTest(false),
			SSLNotifyOnChange: boolPtrForTest(true),
			DomainAlertDays:   intPtrForTest(-1),
			DomainExpiration:  intPtrForTest(200),
		}
		model := &MonitorResourceModel{}
		var diags diag.Diagnostics
		r.mapMonitorToModel(monitor, model, &diags)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		assertInt64(t, "ssl_expiration", model.SSLExpiration, 41)
		assertInt64(t, "ssl_alert_days", model.SSLAlertDays, 30)
		assertBool(t, "ssl_reminders", model.SSLReminders, false)
		assertBool(t, "ssl_notify_on_change", model.SSLNotifyOnChange, true)
		assertInt64(t, "domain_alert_days", model.DomainAlertDays, -1)
		assertInt64(t, "domain_expiration", model.DomainExpiration, 200)
	})

	t.Run("non-http protocol still maps settings", func(t *testing.T) {
		monitor := &client.Monitor{
			UUID:            "mon-icmp",
			Name:            "ICMP",
			URL:             "example.com",
			Protocol:        "icmp",
			SSLAlertDays:    intPtrForTest(15),
			SSLReminders:    boolPtrForTest(true),
			DomainAlertDays: intPtrForTest(30),
		}
		model := &MonitorResourceModel{}
		var diags diag.Diagnostics
		r.mapMonitorToModel(monitor, model, &diags)
		assertInt64(t, "ssl_alert_days", model.SSLAlertDays, 15)
		assertBool(t, "ssl_reminders", model.SSLReminders, true)
		assertInt64(t, "domain_alert_days", model.DomainAlertDays, 30)
	})

	t.Run("absent fields map to null", func(t *testing.T) {
		monitor := &client.Monitor{UUID: "mon-legacy", Name: "Legacy", URL: "https://example.com", Protocol: "http"}
		model := &MonitorResourceModel{}
		var diags diag.Diagnostics
		r.mapMonitorToModel(monitor, model, &diags)
		if !model.SSLAlertDays.IsNull() || !model.SSLReminders.IsNull() || !model.SSLNotifyOnChange.IsNull() ||
			!model.DomainAlertDays.IsNull() || !model.DomainExpiration.IsNull() {
			t.Error("expected all expiry alert attributes to be null when absent from the API response")
		}
	})
}

func TestMapMonitorCommonFields_ExpiryAlerts(t *testing.T) {
	monitor := &client.Monitor{
		UUID:              "mon-common",
		Name:              "Common",
		URL:               "https://example.com",
		Protocol:          "http",
		SSLAlertDays:      intPtrForTest(60),
		SSLReminders:      boolPtrForTest(true),
		SSLNotifyOnChange: boolPtrForTest(false),
		DomainAlertDays:   intPtrForTest(90),
	}
	var diags diag.Diagnostics
	fields := MapMonitorCommonFields(monitor, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	assertInt64(t, "ssl_alert_days", fields.SSLAlertDays, 60)
	assertBool(t, "ssl_reminders", fields.SSLReminders, true)
	assertBool(t, "ssl_notify_on_change", fields.SSLNotifyOnChange, false)
	assertInt64(t, "domain_alert_days", fields.DomainAlertDays, 90)
	if !fields.DomainExpiration.IsNull() {
		t.Error("expected domain_expiration null when the API returns null")
	}
	if !fields.SSLExpiration.IsNull() {
		t.Error("expected ssl_expiration null when the API returns null")
	}
}

func baseCreatePlan() MonitorResourceModel {
	return MonitorResourceModel{
		Name:               types.StringValue("m"),
		URL:                types.StringValue("https://example.com"),
		Protocol:           types.StringValue("http"),
		HTTPMethod:         types.StringValue("GET"),
		CheckFrequency:     types.Int64Value(60),
		ExpectedStatusCode: types.StringValue("2xx"),
		FollowRedirects:    types.BoolValue(true),
		Regions:            types.ListNull(types.StringType),
		RequestHeaders:     types.ListNull(types.ObjectType{AttrTypes: RequestHeaderAttrTypes()}),
		SSLAlertDays:       types.Int64Unknown(),
		SSLReminders:       types.BoolUnknown(),
		SSLNotifyOnChange:  types.BoolUnknown(),
		DomainAlertDays:    types.Int64Unknown(),
		DomainExpiration:   types.Int64Unknown(),
	}
}

func TestMonitorResource_buildCreateRequest_ExpiryAlerts(t *testing.T) {
	r := &MonitorResource{}
	ctx := context.Background()

	t.Run("omitted in config (unknown plan) are not sent", func(t *testing.T) {
		plan := baseCreatePlan()
		var diags diag.Diagnostics
		req := r.buildCreateRequest(ctx, &plan, &diags)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if req.SSLAlertDays != nil || req.SSLReminders != nil || req.SSLNotifyOnChange != nil || req.DomainAlertDays != nil {
			t.Errorf("expected no expiry alert fields in create request, got %+v", req)
		}
	})

	t.Run("set in config are sent, including -1 and false", func(t *testing.T) {
		plan := baseCreatePlan()
		plan.SSLAlertDays = types.Int64Value(-1)
		plan.SSLReminders = types.BoolValue(false)
		plan.SSLNotifyOnChange = types.BoolValue(true)
		plan.DomainAlertDays = types.Int64Value(30)
		var diags diag.Diagnostics
		req := r.buildCreateRequest(ctx, &plan, &diags)
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
		if req.SSLAlertDays == nil || *req.SSLAlertDays != -1 {
			t.Errorf("expected SSLAlertDays=-1, got %v", req.SSLAlertDays)
		}
		if req.SSLReminders == nil || *req.SSLReminders {
			t.Errorf("expected SSLReminders=false, got %v", req.SSLReminders)
		}
		if req.SSLNotifyOnChange == nil || !*req.SSLNotifyOnChange {
			t.Errorf("expected SSLNotifyOnChange=true, got %v", req.SSLNotifyOnChange)
		}
		if req.DomainAlertDays == nil || *req.DomainAlertDays != 30 {
			t.Errorf("expected DomainAlertDays=30, got %v", req.DomainAlertDays)
		}
	})
}

func expiryState() MonitorResourceModel {
	return MonitorResourceModel{
		SSLAlertDays:      types.Int64Value(15),
		SSLReminders:      types.BoolValue(true),
		SSLNotifyOnChange: types.BoolValue(false),
		DomainAlertDays:   types.Int64Value(-1),
	}
}

func TestApplyExpiryAlertFieldChanges(t *testing.T) {
	t.Run("changed fields are sent", func(t *testing.T) {
		state := expiryState()
		plan := expiryState()
		plan.SSLAlertDays = types.Int64Value(30)
		plan.SSLReminders = types.BoolValue(false)
		plan.SSLNotifyOnChange = types.BoolValue(true)
		plan.DomainAlertDays = types.Int64Value(30)

		req := client.UpdateMonitorRequest{}
		applyExpiryAlertFieldChanges(&plan, &state, &req)

		if req.SSLAlertDays == nil || *req.SSLAlertDays != 30 {
			t.Errorf("expected SSLAlertDays=30, got %v", req.SSLAlertDays)
		}
		if req.SSLReminders == nil || *req.SSLReminders {
			t.Errorf("expected SSLReminders=false, got %v", req.SSLReminders)
		}
		if req.SSLNotifyOnChange == nil || !*req.SSLNotifyOnChange {
			t.Errorf("expected SSLNotifyOnChange=true, got %v", req.SSLNotifyOnChange)
		}
		if req.DomainAlertDays == nil || *req.DomainAlertDays != 30 {
			t.Errorf("expected DomainAlertDays=30, got %v", req.DomainAlertDays)
		}
	})

	t.Run("changing to never (-1) is sent", func(t *testing.T) {
		state := expiryState()
		plan := expiryState()
		plan.SSLAlertDays = types.Int64Value(-1)

		req := client.UpdateMonitorRequest{}
		applyExpiryAlertFieldChanges(&plan, &state, &req)

		if req.SSLAlertDays == nil || *req.SSLAlertDays != -1 {
			t.Errorf("expected SSLAlertDays=-1, got %v", req.SSLAlertDays)
		}
		if req.SSLReminders != nil || req.SSLNotifyOnChange != nil || req.DomainAlertDays != nil {
			t.Error("expected only ssl_alert_days to be sent")
		}
	})

	t.Run("unchanged fields are omitted", func(t *testing.T) {
		state := expiryState()
		plan := expiryState()

		req := client.UpdateMonitorRequest{}
		applyExpiryAlertFieldChanges(&plan, &state, &req)

		if req.SSLAlertDays != nil || req.SSLReminders != nil || req.SSLNotifyOnChange != nil || req.DomainAlertDays != nil {
			t.Errorf("expected no expiry alert fields when unchanged, got %+v", req)
		}
	})

	t.Run("unknown plan values (e.g. state from an older provider) are not sent", func(t *testing.T) {
		state := MonitorResourceModel{
			SSLAlertDays:      types.Int64Null(),
			SSLReminders:      types.BoolNull(),
			SSLNotifyOnChange: types.BoolNull(),
			DomainAlertDays:   types.Int64Null(),
		}
		plan := MonitorResourceModel{
			SSLAlertDays:      types.Int64Unknown(),
			SSLReminders:      types.BoolUnknown(),
			SSLNotifyOnChange: types.BoolUnknown(),
			DomainAlertDays:   types.Int64Unknown(),
		}

		req := client.UpdateMonitorRequest{}
		applyExpiryAlertFieldChanges(&plan, &state, &req)

		if req.SSLAlertDays != nil || req.SSLReminders != nil || req.SSLNotifyOnChange != nil || req.DomainAlertDays != nil {
			t.Errorf("expected no expiry alert fields for unknown plan values, got %+v", req)
		}
	})
}

func TestMonitorResource_buildUpdateRequest_ExpiryAlertsOnly(t *testing.T) {
	r := &MonitorResource{}
	ctx := context.Background()

	state := baseCreatePlan()
	state.SSLAlertDays = types.Int64Value(15)
	state.SSLReminders = types.BoolValue(true)
	state.SSLNotifyOnChange = types.BoolValue(false)
	state.DomainAlertDays = types.Int64Value(-1)
	state.DomainExpiration = types.Int64Null()

	plan := state
	plan.SSLAlertDays = types.Int64Value(30)
	plan.DomainAlertDays = types.Int64Value(30)

	var diags diag.Diagnostics
	req := r.buildUpdateRequest(ctx, &plan, &state, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if req.SSLAlertDays == nil || *req.SSLAlertDays != 30 {
		t.Errorf("expected SSLAlertDays=30, got %v", req.SSLAlertDays)
	}
	if req.DomainAlertDays == nil || *req.DomainAlertDays != 30 {
		t.Errorf("expected DomainAlertDays=30, got %v", req.DomainAlertDays)
	}
	if req.SSLReminders != nil || req.SSLNotifyOnChange != nil {
		t.Error("expected unchanged ssl_reminders / ssl_notify_on_change to be omitted")
	}
	if req.Name != nil || req.URL != nil || req.CheckFrequency != nil || req.AlertsWait != nil {
		t.Error("expected unrelated fields to be omitted")
	}
}

func TestMonitorDataSources_Schema_ExpiryAlertAttributes(t *testing.T) {
	ctx := context.Background()

	single := &datasource.SchemaResponse{}
	(&MonitorDataSource{}).Schema(ctx, datasource.SchemaRequest{}, single)
	for _, name := range expiryAllAttrs {
		attr, ok := single.Schema.Attributes[name]
		if !ok {
			t.Errorf("hyperping_monitor data source missing %q", name)
			continue
		}
		if !attr.IsComputed() || attr.IsOptional() || attr.IsRequired() {
			t.Errorf("hyperping_monitor data source %q must be Computed only", name)
		}
	}

	list := &datasource.SchemaResponse{}
	(&MonitorsDataSource{}).Schema(ctx, datasource.SchemaRequest{}, list)
	monitors, ok := list.Schema.Attributes["monitors"].(dsschema.ListNestedAttribute)
	if !ok {
		t.Fatal("hyperping_monitors data source: monitors is not a ListNestedAttribute")
	}
	for _, name := range expiryAllAttrs {
		attr, ok := monitors.NestedObject.Attributes[name]
		if !ok {
			t.Errorf("hyperping_monitors data source missing monitors.%s", name)
			continue
		}
		if !attr.IsComputed() {
			t.Errorf("hyperping_monitors data source monitors.%s must be Computed", name)
		}
	}
}

func TestMonitorDataSources_mapping_ExpiryAlerts(t *testing.T) {
	monitor := &client.Monitor{
		UUID:              "mon-ds",
		Name:              "DS",
		URL:               "https://example.com",
		Protocol:          "http",
		SSLExpiration:     intPtrForTest(12),
		SSLAlertDays:      intPtrForTest(7),
		SSLReminders:      boolPtrForTest(true),
		SSLNotifyOnChange: boolPtrForTest(true),
		DomainAlertDays:   intPtrForTest(14),
		DomainExpiration:  intPtrForTest(365),
	}

	var diags diag.Diagnostics
	single := &MonitorDataSourceModel{}
	(&MonitorDataSource{}).mapMonitorToDataSourceModel(monitor, single, &diags)
	listItem := &MonitorDataModel{}
	(&MonitorsDataSource{}).mapMonitorToDataModel(monitor, listItem, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	for label, got := range map[string][6]any{
		"monitor":  {single.SSLExpiration, single.SSLAlertDays, single.SSLReminders, single.SSLNotifyOnChange, single.DomainAlertDays, single.DomainExpiration},
		"monitors": {listItem.SSLExpiration, listItem.SSLAlertDays, listItem.SSLReminders, listItem.SSLNotifyOnChange, listItem.DomainAlertDays, listItem.DomainExpiration},
	} {
		t.Run(label, func(t *testing.T) {
			assertInt64(t, "ssl_expiration", got[0].(types.Int64), 12)
			assertInt64(t, "ssl_alert_days", got[1].(types.Int64), 7)
			assertBool(t, "ssl_reminders", got[2].(types.Bool), true)
			assertBool(t, "ssl_notify_on_change", got[3].(types.Bool), true)
			assertInt64(t, "domain_alert_days", got[4].(types.Int64), 14)
			assertInt64(t, "domain_expiration", got[5].(types.Int64), 365)
		})
	}
}

func intPtrForTest(i int) *int    { return &i }
func boolPtrForTest(b bool) *bool { return &b }

func assertInt64(t *testing.T, name string, got types.Int64, want int64) {
	t.Helper()
	if got.IsNull() || got.IsUnknown() {
		t.Errorf("%s: expected %d, got null/unknown", name, want)
		return
	}
	if got.ValueInt64() != want {
		t.Errorf("%s: expected %d, got %d", name, want, got.ValueInt64())
	}
}

func assertBool(t *testing.T, name string, got types.Bool, want bool) {
	t.Helper()
	if got.IsNull() || got.IsUnknown() {
		t.Errorf("%s: expected %v, got null/unknown", name, want)
		return
	}
	if got.ValueBool() != want {
		t.Errorf("%s: expected %v, got %v", name, want, got.ValueBool())
	}
}
