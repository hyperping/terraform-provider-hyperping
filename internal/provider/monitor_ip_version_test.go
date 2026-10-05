// Copyright (c) 2026 Develeap
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/develeap/terraform-provider-hyperping/internal/client"
)

// Tests for ip_version on hyperping_monitor (Optional+Computed, 4 or 6).

func TestMonitorResource_Schema_IPVersion(t *testing.T) {
	s := monitorResourceSchema(t)

	attr, ok := s.Attributes["ip_version"].(schema.Int64Attribute)
	if !ok {
		t.Fatal("ip_version must be an Int64Attribute")
	}
	if !attr.IsOptional() || !attr.IsComputed() || attr.IsRequired() {
		t.Errorf("ip_version must be Optional+Computed (optional=%v computed=%v required=%v)",
			attr.IsOptional(), attr.IsComputed(), attr.IsRequired())
	}
	// No provider-side default: a monitor switched to IPv6 in the dashboard must
	// not be flipped back to IPv4 by a config that doesn't mention ip_version.
	if attr.Default != nil {
		t.Error("ip_version must not have a provider-side default")
	}

	for _, v := range []int64{4, 6} {
		if diags := runInt64Validators(attr.Validators, v); diags.HasError() {
			t.Errorf("ip_version=%d should be valid, got %v", v, diags)
		}
	}
	for _, v := range []int64{0, 1, 5, 46, -4} {
		if diags := runInt64Validators(attr.Validators, v); !diags.HasError() {
			t.Errorf("ip_version=%d should be rejected", v)
		}
	}
}

func TestMonitorResource_mapMonitorToModel_IPVersion(t *testing.T) {
	r := &MonitorResource{}

	for _, tt := range []struct {
		name string
		in   *int
		want types.Int64
	}{
		{"ipv6", intPtrForTest(6), types.Int64Value(6)},
		{"ipv4", intPtrForTest(4), types.Int64Value(4)},
		{"absent (older API)", nil, types.Int64Null()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			monitor := &client.Monitor{UUID: "mon-ip", Name: "IP", URL: "https://example.com", Protocol: "http", IPVersion: tt.in}
			var model MonitorResourceModel
			var diags diag.Diagnostics
			r.mapMonitorToModel(monitor, &model, &diags)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if !model.IPVersion.Equal(tt.want) {
				t.Errorf("IPVersion = %v, want %v", model.IPVersion, tt.want)
			}
		})
	}
}

func TestMonitorResource_buildCreateRequest_IPVersion(t *testing.T) {
	r := &MonitorResource{}
	ctx := context.Background()

	t.Run("omitted in config is not sent", func(t *testing.T) {
		plan := baseCreatePlan()
		var diags diag.Diagnostics
		req := r.buildCreateRequest(ctx, &plan, &diags)
		if req.IPVersion != nil {
			t.Errorf("expected no ip_version, got %d", *req.IPVersion)
		}
		body, _ := json.Marshal(req)
		if strings.Contains(string(body), "ip_version") {
			t.Errorf("ip_version must be omitted from the JSON body: %s", body)
		}
	})

	t.Run("set in config is sent", func(t *testing.T) {
		plan := baseCreatePlan()
		plan.IPVersion = types.Int64Value(6)
		var diags diag.Diagnostics
		req := r.buildCreateRequest(ctx, &plan, &diags)
		if req.IPVersion == nil || *req.IPVersion != 6 {
			t.Errorf("expected IPVersion=6, got %v", req.IPVersion)
		}
	})
}

func TestApplyMonitoringFieldChanges_IPVersion(t *testing.T) {
	ctx := context.Background()

	t.Run("changed value is sent", func(t *testing.T) {
		plan := MonitorResourceModel{IPVersion: types.Int64Value(6)}
		state := MonitorResourceModel{IPVersion: types.Int64Value(4)}
		var req client.UpdateMonitorRequest
		var diags diag.Diagnostics
		applyMonitoringFieldChanges(ctx, &plan, &state, &req, &diags)
		if req.IPVersion == nil || *req.IPVersion != 6 {
			t.Errorf("expected IPVersion=6, got %v", req.IPVersion)
		}
	})

	t.Run("unchanged value is not sent", func(t *testing.T) {
		plan := MonitorResourceModel{IPVersion: types.Int64Value(6)}
		state := MonitorResourceModel{IPVersion: types.Int64Value(6)}
		var req client.UpdateMonitorRequest
		var diags diag.Diagnostics
		applyMonitoringFieldChanges(ctx, &plan, &state, &req, &diags)
		if req.IPVersion != nil {
			t.Errorf("expected no ip_version, got %d", *req.IPVersion)
		}
	})
}
