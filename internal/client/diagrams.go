// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Diagnostic is one located problem the API found, in a file it imported or
// in Plotwork source.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Hint     string `json:"hint,omitempty"`
}

// String formats the diagnostic as one line: "I201 3:5 message. Hint: ...".
func (d Diagnostic) String() string {
	s := fmt.Sprintf("%s %d:%d %s", d.Code, d.Line, d.Column, d.Message)
	if d.Hint != "" {
		s += " Hint: " + d.Hint
	}
	return s
}

// Size is the natural size of a laid-out diagram, in pixels.
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ImportFile is one file of a folder sent to an import.
type ImportFile struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// ImportOptions are the import options the provider exposes. The API takes
// more; they are added here when a provider attribute needs them.
type ImportOptions struct {
	View     string   `json:"view,omitempty"`
	GroupBy  string   `json:"groupBy,omitempty"`
	Root     string   `json:"root,omitempty"`
	VarFiles []string `json:"varFiles,omitempty"`
	Icons    string   `json:"icons,omitempty"`
}

// ImportRequest is the body of POST /v1/import. Send Text or Files, not both.
type ImportRequest struct {
	Format  string        `json:"format"`
	Text    string        `json:"text,omitempty"`
	Files   []ImportFile  `json:"files,omitempty"`
	Title   string        `json:"title,omitempty"`
	Options ImportOptions `json:"options"`
}

// Coverage says how much of the imported file the diagram shows.
type Coverage struct {
	Complete bool   `json:"complete"`
	Line     string `json:"line"`
}

// Validation is the API's check of the imported source.
type Validation struct {
	Summary  *string      `json:"summary"`
	Size     *Size        `json:"size"`
	Warnings []Diagnostic `json:"warnings"`
}

// ImportResponse is the answer of POST /v1/import. A file that cannot be read
// is not an HTTP error: OK is false and Diagnostics say why.
type ImportResponse struct {
	OK          bool         `json:"ok"`
	Kind        *string      `json:"kind"`
	Source      string       `json:"source"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Coverage    *Coverage    `json:"coverage"`
	Validation  *Validation  `json:"validation"`
	PartsTotal  int          `json:"partsTotal"`
	Outline     []string     `json:"outline"`
}

// Import turns the text of a file (Terraform, Kubernetes manifests, SQL and
// the other formats the API reads) into Plotwork source.
func (c *Client) Import(ctx context.Context, req ImportRequest) (*ImportResponse, error) {
	var out ImportResponse
	if err := c.do(ctx, http.MethodPost, "/v1/import", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RenderRequest is the body of POST /v1/diagrams/render.
type RenderRequest struct {
	Source      string `json:"source"`
	Scheme      string `json:"scheme,omitempty"`
	Theme       string `json:"theme,omitempty"`
	EmbedSource bool   `json:"embedSource"`
}

// RenderResponse is a rendered diagram.
type RenderResponse struct {
	SVG      string       `json:"svg"`
	Scheme   string       `json:"scheme"`
	Size     Size         `json:"size"`
	Warnings []Diagnostic `json:"warnings"`
}

// Render draws Plotwork source as a self-contained SVG. Source with errors is
// an *APIError of status 422 whose Details hold the diagnostics.
func (c *Client) Render(ctx context.Context, req RenderRequest) (*RenderResponse, error) {
	var out RenderResponse
	if err := c.do(ctx, http.MethodPost, "/v1/diagrams/render", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PublishRequest is the body of a publish. Organization, Personal and Alias
// are for the personal route only and are left out of an organisation's.
type PublishRequest struct {
	Source         string  `json:"source"`
	Scheme         string  `json:"scheme,omitempty"`
	EmbedSource    *bool   `json:"embedSource,omitempty"`
	Name           string  `json:"name,omitempty"`
	Folder         *string `json:"folder"` // null files the diagram at the top level
	Classification string  `json:"classification,omitempty"`
	Alias          string  `json:"alias,omitempty"`
	Organization   string  `json:"organization,omitempty"`
	Personal       bool    `json:"personal,omitempty"`
}

// Organization names the organisation whose private library holds a diagram.
type Organization struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// StableLink is an address that always shows the latest diagram published
// under its alias.
type StableLink struct {
	ID    string `json:"id"`
	Alias string `json:"alias"`
	URL   string `json:"url"`
}

// Diagram is a published diagram, in a person's library (with a public URL)
// or in an organisation's private library (with a ViewURL for members).
type Diagram struct {
	ID          string        `json:"id"`
	URL         string        `json:"url,omitempty"`
	ViewURL     string        `json:"viewUrl,omitempty"`
	Name        *string       `json:"name"`
	Folder      *string       `json:"folder"`
	Scheme      string        `json:"scheme"`
	Bytes       int64         `json:"bytes"`
	PublishedAt int64         `json:"publishedAt"`
	ExpiresAt   *int64        `json:"expiresAt"`
	Locked      bool          `json:"locked"`
	Created     bool          `json:"created"`
	Pending     bool          `json:"pending,omitempty"`
	Private     bool          `json:"private,omitempty"`
	Org         *Organization `json:"organization,omitempty"`
	StableLink  *StableLink   `json:"stableLink,omitempty"`
}

// Publish stores a diagram in the caller's own library and returns its public
// link. When an organisation the caller belongs to routes its members'
// diagrams to its library, the answer is that library's diagram instead:
// Private is true and Org names the organisation. Publishing the same source
// again returns the diagram already stored, with Created false.
func (c *Client) Publish(ctx context.Context, req PublishRequest) (*Diagram, error) {
	var out Diagram
	if err := c.do(ctx, http.MethodPost, "/v1/diagrams/publish", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PublishToOrganization stores a diagram in an organisation's private
// library. org is the organisation's slug or id.
func (c *Client) PublishToOrganization(ctx context.Context, org string, req PublishRequest) (*Diagram, error) {
	req.Alias, req.Organization, req.Personal = "", "", false
	var out Diagram
	if err := c.do(ctx, http.MethodPost, "/orgs/"+url.PathEscape(org)+"/diagrams", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDiagram removes one of the caller's own diagrams. A diagram that is
// already gone is an error for which IsNotFound is true.
func (c *Client) DeleteDiagram(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/user/diagrams/"+url.PathEscape(id), nil, nil)
}

// DeleteOrganizationDiagram removes a diagram from an organisation's library.
func (c *Client) DeleteOrganizationDiagram(ctx context.Context, org, id string) error {
	return c.do(ctx, http.MethodDelete, "/orgs/"+url.PathEscape(org)+"/diagrams/"+url.PathEscape(id), nil, nil)
}

// OrganizationDiagramExists reports whether an organisation's library still
// holds a diagram. It asks for the picture, which every role may read.
func (c *Client) OrganizationDiagramExists(ctx context.Context, org, id string) (bool, error) {
	err := c.do(ctx, http.MethodGet, "/orgs/"+url.PathEscape(org)+"/diagrams/"+url.PathEscape(id)+"/svg", nil, nil)
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// PublicDiagramExists reports whether a public diagram link still answers.
// The link is on the CDN host, so the request carries no API key. A plan
// that expires diagrams removes them from the CDN, and that is how the
// provider sees a diagram expire.
func (c *Client) PublicDiagramExists(ctx context.Context, link string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, link, nil)
	if err != nil {
		return false, fmt.Errorf("building HEAD %s: %w", link, err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("HEAD %s: %w", link, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return false, nil
	case resp.StatusCode < 400:
		return true, nil
	default:
		return false, &APIError{StatusCode: resp.StatusCode}
	}
}
