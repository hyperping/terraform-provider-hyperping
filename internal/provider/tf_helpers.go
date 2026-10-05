// Copyright (c) 2026 Develeap
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Terraform type conversion helpers - reduces code duplication across resources

// isNullOrUnknown checks if an attribute value is null or unknown.
// This is a common pattern used throughout the codebase for conditional logic.
func isNullOrUnknown(v attr.Value) bool {
	return v.IsNull() || v.IsUnknown()
}

// tfStringToPtr converts a types.String to *string.
// Returns nil if the value is null or unknown, otherwise returns a pointer to the string value.
func tfStringToPtr(v types.String) *string {
	if isNullOrUnknown(v) {
		return nil
	}
	s := v.ValueString()
	return &s
}

// tfIntToPtr converts a types.Int64 to *int.
// Returns nil if the value is null or unknown, otherwise returns a pointer to the int value.
func tfIntToPtr(v types.Int64) *int {
	if isNullOrUnknown(v) {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

// tfBoolToPtr converts a types.Bool to *bool.
// Returns nil if the value is null or unknown, otherwise returns a pointer to the bool value.
func tfBoolToPtr(v types.Bool) *bool {
	if isNullOrUnknown(v) {
		return nil
	}
	b := v.ValueBool()
	return &b
}

// intPtrToTF converts *int to types.Int64.
// Returns null if pointer is nil, otherwise returns the value (zero and negative values included).
func intPtrToTF(i *int) types.Int64 {
	if i == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*i))
}

// boolPtrToTF converts *bool to types.Bool.
// Returns null if pointer is nil, otherwise returns the bool value (false included).
func boolPtrToTF(b *bool) types.Bool {
	if b == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*b)
}

// stringOrNull returns types.String with value if string is non-empty, otherwise null.
func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
