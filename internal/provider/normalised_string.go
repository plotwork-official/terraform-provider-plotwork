// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// normaliser turns a value into the one form the API compares, and reports
// whether the value can be normalised at all.
type normaliser string

const (
	// normaliseColour reads #rgb or #rrggbb in any case as lower-case #rrggbb.
	normaliseColour normaliser = "colour"
	// normaliseJSON reads JSON text as its compact form with sorted keys, so
	// whitespace and key order do not count.
	normaliseJSON normaliser = "json"
)

// hexColour is a colour as the brand routes take it: #rgb or #rrggbb.
var hexColour = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// normalise applies n to s.
func (n normaliser) normalise(s string) (string, bool) {
	switch n {
	case normaliseColour:
		if !hexColour.MatchString(s) {
			return "", false
		}
		s = strings.ToLower(s)
		if len(s) == 4 {
			s = string([]byte{'#', s[1], s[1], s[2], s[2], s[3], s[3]})
		}
		return s, true
	case normaliseJSON:
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return "", false
		}
		compact, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(compact), true
	default:
		return s, true
	}
}

var (
	_ basetypes.StringTypable                    = normalisedStringType{}
	_ basetypes.StringValuableWithSemanticEquals = normalisedString{}
)

// normalisedStringType is a string attribute whose values are equal when
// their normalised forms are, so a value the API writes differently from the
// configuration (#ABC for #aabbcc, JSON with other spacing) is no change.
type normalisedStringType struct {
	basetypes.StringType
	n normaliser
}

// Equal reports whether o is the same type with the same normaliser.
func (t normalisedStringType) Equal(o attr.Type) bool {
	other, ok := o.(normalisedStringType)
	return ok && other.n == t.n
}

// String names the type in framework messages.
func (t normalisedStringType) String() string {
	return "normalisedStringType(" + string(t.n) + ")"
}

// ValueFromString wraps a plain string value.
func (t normalisedStringType) ValueFromString(_ context.Context, in basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return normalisedString{StringValue: in, n: t.n}, nil
}

// ValueFromTerraform reads a value from Terraform's wire form.
func (t normalisedStringType) ValueFromTerraform(ctx context.Context, in tftypes.Value) (attr.Value, error) {
	v, err := t.StringType.ValueFromTerraform(ctx, in)
	if err != nil {
		return nil, err
	}
	s, ok := v.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected value type %T", v)
	}
	return normalisedString{StringValue: s, n: t.n}, nil
}

// ValueType is the value type of this type.
func (t normalisedStringType) ValueType(_ context.Context) attr.Value {
	return normalisedString{n: t.n}
}

// normalisedString is a value of normalisedStringType.
type normalisedString struct {
	basetypes.StringValue
	n normaliser
}

// Equal reports exact equality, as Terraform needs; StringSemanticEquals is
// the looser one.
func (v normalisedString) Equal(o attr.Value) bool {
	other, ok := o.(normalisedString)
	return ok && other.n == v.n && v.StringValue.Equal(other.StringValue)
}

// Type is the value's type.
func (v normalisedString) Type(_ context.Context) attr.Type {
	return normalisedStringType{n: v.n}
}

// StringSemanticEquals reports whether two known values normalise to the
// same text. The framework then keeps the prior value in state.
func (v normalisedString) StringSemanticEquals(_ context.Context, newValuable basetypes.StringValuable) (bool, diag.Diagnostics) {
	other, ok := newValuable.(normalisedString)
	if !ok {
		return false, nil
	}
	return v.sameAs(other), nil
}

// sameAs reports whether both values are known and normalise alike, or both
// are null.
func (v normalisedString) sameAs(o normalisedString) bool {
	if v.IsNull() || o.IsNull() {
		return v.IsNull() && o.IsNull()
	}
	if v.IsUnknown() || o.IsUnknown() {
		return false
	}
	a, okA := v.n.normalise(v.ValueString())
	b, okB := v.n.normalise(o.ValueString())
	return okA && okB && a == b
}

// normalised is the value's normalised text, or "" when it is null,
// unknown or cannot be read.
func (v normalisedString) normalised() string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	s, _ := v.n.normalise(v.ValueString())
	return s
}

// colourValue is a colour attribute's value.
func colourValue(s string) normalisedString {
	return normalisedString{StringValue: types.StringValue(s), n: normaliseColour}
}

// colourNull is a colour attribute with no value.
func colourNull() normalisedString {
	return normalisedString{StringValue: types.StringNull(), n: normaliseColour}
}

// jsonValue is a JSON text attribute's value.
func jsonValue(s string) normalisedString {
	return normalisedString{StringValue: types.StringValue(s), n: normaliseJSON}
}
