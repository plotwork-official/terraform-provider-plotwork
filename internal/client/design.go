// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// The routes in this file change an organisation's design: its brand theme,
// its custom icons and its approved patterns. An organisation key needs the
// design:write scope for them, which the organisation's owner or an admin
// gives it; without it the API answers 403 orgs.forbidden.

// BrandColors are the three colours of a brand theme, each lower-case
// #rrggbb.
type BrandColors struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
	Accent    string `json:"accent"`
}

// Brand is an organisation's brand theme as GET /orgs/{org}/brand answers.
type Brand struct {
	// Brand is the saved colours, or nil when the organisation has none.
	Brand *BrandColors `json:"brand"`
	// Chosen names the colours a person chose: "primary", "secondary",
	// "accent". The others are made from the primary.
	Chosen []string `json:"chosen"`
	// Active is true when the owner's plan paints with the brand.
	Active bool `json:"active"`
	// CanEdit is true when the caller may change the brand.
	CanEdit bool `json:"canEdit"`
	// Plan is the organisation's plan, which is its owner's.
	Plan string `json:"plan"`
}

// BrandRequest is the body of PUT /orgs/{org}/brand. A nil Secondary or
// Accent is sent as null, so the API makes it from the primary.
type BrandRequest struct {
	Primary   string  `json:"primary"`
	Secondary *string `json:"secondary"`
	Accent    *string `json:"accent"`
}

// BrandIssue is one thing the API's check says about one brand colour.
type BrandIssue struct {
	Role     string `json:"role"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// SavedBrand is the answer of PUT /orgs/{org}/brand.
type SavedBrand struct {
	Brand  BrandColors `json:"brand"`
	Chosen []string    `json:"chosen"`
	Check  struct {
		Issues []BrandIssue `json:"issues"`
	} `json:"check"`
}

// GetBrand reads an organisation's brand theme. org is its slug or id.
func (c *Client) GetBrand(ctx context.Context, org string) (*Brand, error) {
	var out Brand
	if err := c.do(ctx, http.MethodGet, orgPath(org, "brand"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SaveBrand saves an organisation's brand theme. Colours the API cannot read
// are an *APIError of status 422, code brand.invalid, whose Details hold
// every BrandIssue.
func (c *Client) SaveBrand(ctx context.Context, org string, req BrandRequest) (*SavedBrand, error) {
	var out SavedBrand
	if err := c.do(ctx, http.MethodPut, orgPath(org, "brand"), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteBrand removes an organisation's brand theme. Removing one that is
// not there succeeds.
func (c *Client) DeleteBrand(ctx context.Context, org string) error {
	return c.do(ctx, http.MethodDelete, orgPath(org, "brand"), nil, nil)
}

// Icon is one of an organisation's custom icons.
type Icon struct {
	Name string `json:"name"`
	// Ref is what a diagram writes to use the icon: "org:<name>".
	Ref       string   `json:"ref"`
	Aliases   []string `json:"aliases"`
	MediaType string   `json:"mediaType"`
	// Image is the checked picture as a data: address.
	Image  string  `json:"image"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Bytes  int64   `json:"bytes"`
	// UpdatedAt is milliseconds since the epoch.
	UpdatedAt int64 `json:"updatedAt"`
}

// Icons is the answer of GET /orgs/{org}/icons.
type Icons struct {
	Icons   []Icon `json:"icons"`
	Active  bool   `json:"active"`
	CanEdit bool   `json:"canEdit"`
	Plan    string `json:"plan"`
	Limit   int    `json:"limit"`
}

// IconRequest is the body of PUT /orgs/{org}/icons/{name}. Send one file,
// SVG or PNG, or neither to change only the aliases of an icon that exists.
// Aliases replace the icon's; an empty list removes them.
type IconRequest struct {
	SVG     string   `json:"svg,omitempty"`
	PNG     string   `json:"png,omitempty"`
	Aliases []string `json:"aliases"`
}

// SavedIcon is the answer of PUT /orgs/{org}/icons/{name}.
type SavedIcon struct {
	Icon Icon `json:"icon"`
	// Removed lists what the check left out of the file because it draws
	// nothing, such as editor metadata.
	Removed []string `json:"removed"`
}

// IconIssue is one problem the API's upload check found with an icon.
type IconIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// ListIcons reads an organisation's custom icons.
func (c *Client) ListIcons(ctx context.Context, org string) (*Icons, error) {
	var out Icons
	if err := c.do(ctx, http.MethodGet, orgPath(org, "icons"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetIcon finds one icon by name in ListIcons. It returns nil and no error
// when the organisation has no icon of that name.
func (c *Client) GetIcon(ctx context.Context, org, name string) (*Icon, error) {
	icons, err := c.ListIcons(ctx, org)
	if err != nil {
		return nil, err
	}
	for i := range icons.Icons {
		if icons.Icons[i].Name == name {
			return &icons.Icons[i], nil
		}
	}
	return nil, nil
}

// SaveIcon uploads or replaces one icon. A file or name the API's check
// refuses is an *APIError of status 422, code icon.invalid, whose Details
// hold every IconIssue.
func (c *Client) SaveIcon(ctx context.Context, org, name string, req IconRequest) (*SavedIcon, error) {
	if req.Aliases == nil {
		req.Aliases = []string{}
	}
	var out SavedIcon
	if err := c.do(ctx, http.MethodPut, orgPath(org, "icons", name), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteIcon removes one icon.
func (c *Client) DeleteIcon(ctx context.Context, org, name string) error {
	return c.do(ctx, http.MethodDelete, orgPath(org, "icons", name), nil, nil)
}

// PatternSlot is one {{slot}} of a pattern: what a team fills in.
type PatternSlot struct {
	ID      string `json:"id"`
	Default string `json:"default"`
	Meaning string `json:"meaning"`
}

// PatternNotes say what to keep, rename and split in a pattern.
type PatternNotes struct {
	Keep   string `json:"keep"`
	Rename string `json:"rename"`
	Split  string `json:"split"`
}

// Pattern is one of an organisation's approved patterns, as the API answers.
type Pattern struct {
	// ID is what the tools take: "org:<name>".
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Title     string            `json:"title"`
	Summary   string            `json:"summary"`
	WhenToUse string            `json:"whenToUse"`
	Kind      string            `json:"kind"`
	Tags      []string          `json:"tags"`
	Flavours  []string          `json:"flavours"`
	Templates map[string]string `json:"templates"`
	Slots     []PatternSlot     `json:"slots"`
	Notes     PatternNotes      `json:"notes"`
	CreatedAt int64             `json:"createdAt"`
	// UpdatedAt is milliseconds since the epoch of the last save.
	UpdatedAt int64 `json:"updatedAt"`
}

// PatternProblem is one problem the API found in a pattern file.
type PatternProblem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// GetPattern reads one pattern by its name after "org:". A pattern that is
// not there is an error for which IsNotFound is true.
func (c *Client) GetPattern(ctx context.Context, org, name string) (*Pattern, error) {
	var out Pattern
	if err := c.do(ctx, http.MethodGet, orgPath(org, "patterns", name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreatePattern saves a new pattern. file is a pattern file as JSON, with
// its name; the API ignores $schema. A name the organisation already has is
// an *APIError of code orgs.pattern_exists; a file with problems is code
// orgs.pattern_invalid, whose Details hold every PatternProblem.
func (c *Client) CreatePattern(ctx context.Context, org string, file json.RawMessage) (*Pattern, error) {
	var out Pattern
	if err := c.do(ctx, http.MethodPost, orgPath(org, "patterns"), file, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePattern replaces a pattern, keeping its name. fields is a pattern
// file as JSON without its name or $schema.
func (c *Client) UpdatePattern(ctx context.Context, org, name string, fields json.RawMessage) (*Pattern, error) {
	var out Pattern
	if err := c.do(ctx, http.MethodPut, orgPath(org, "patterns", name), fields, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePattern removes a pattern. One that is already gone is an error for
// which IsNotFound is true.
func (c *Client) DeletePattern(ctx context.Context, org, name string) error {
	return c.do(ctx, http.MethodDelete, orgPath(org, "patterns", name), nil, nil)
}

// orgPath is /orgs/{org}/... with each part escaped.
func orgPath(org string, parts ...string) string {
	p := "/orgs/" + url.PathEscape(org)
	for _, part := range parts {
		p += "/" + url.PathEscape(part)
	}
	return p
}
