// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSaveBrandSendsNullForDerivedColours(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.EscapedPath() != "/orgs/the%20team/brand" {
			t.Errorf("request = %s %s, want PUT /orgs/the%%20team/brand", r.Method, r.URL.EscapedPath())
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["primary"] != "#2a78d6" {
			t.Errorf("primary = %v", body["primary"])
		}
		for _, role := range []string{"secondary", "accent"} {
			if v, ok := body[role]; !ok || v != nil {
				t.Errorf("%s = %v, %v; want an explicit null", role, v, ok)
			}
		}
		_, _ = w.Write([]byte(`{"brand":{"primary":"#2a78d6","secondary":"#7c3aed","accent":"#0ea5a5"},"chosen":["primary"],"check":{"issues":[],"swatches":null}}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv, "k").SaveBrand(context.Background(), "the team", BrandRequest{Primary: "#2a78d6"})
	if err != nil {
		t.Fatalf("SaveBrand: %v", err)
	}
	if got.Brand.Secondary != "#7c3aed" || len(got.Chosen) != 1 {
		t.Errorf("SaveBrand = %+v", got)
	}
}

func TestGetBrandReadsNoBrand(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"brand":null,"chosen":[],"active":false,"canEdit":true,"plan":"pro"}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv, "k").GetBrand(context.Background(), "acme")
	if err != nil || got.Brand != nil || got.Plan != "pro" {
		t.Fatalf("GetBrand = %+v, %v; want no brand on pro", got, err)
	}
}

func TestSaveIconReadsTheWrappedAnswer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/orgs/acme/icons/payments-api" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["png"]; ok {
			t.Error("body has png beside svg")
		}
		if aliases, ok := body["aliases"].([]any); !ok || len(aliases) != 0 {
			t.Errorf("aliases = %v, want an empty list that clears them", body["aliases"])
		}
		_, _ = w.Write([]byte(`{"icon":{"name":"payments-api","ref":"org:payments-api","aliases":[],"mediaType":"image/svg+xml","image":"data:image/svg+xml;base64,","width":24,"height":24,"bytes":120,"updatedAt":5},"removed":["metadata"]}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv, "k").SaveIcon(context.Background(), "acme", "payments-api", IconRequest{SVG: "<svg/>"})
	if err != nil {
		t.Fatalf("SaveIcon: %v", err)
	}
	if got.Icon.Ref != "org:payments-api" || got.Icon.Bytes != 120 || got.Icon.UpdatedAt != 5 || len(got.Removed) != 1 {
		t.Errorf("SaveIcon = %+v", got)
	}
}

func TestGetIconFindsOneByName(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"icons":[{"name":"a","ref":"org:a","aliases":["x"],"mediaType":"image/svg+xml","image":"","width":1,"height":1,"bytes":1,"updatedAt":1}],"active":true,"canEdit":true,"plan":"pro","limit":100}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, "k")
	if got, err := c.GetIcon(context.Background(), "acme", "a"); err != nil || got == nil || got.Aliases[0] != "x" {
		t.Errorf("GetIcon(a) = %+v, %v", got, err)
	}
	if got, err := c.GetIcon(context.Background(), "acme", "b"); err != nil || got != nil {
		t.Errorf("GetIcon(b) = %+v, %v; want nil, nil", got, err)
	}
}

func TestPatternRoutes(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /orgs/acme/patterns":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":true,"errorCode":"orgs.pattern_exists","message":"taken"}`))
		case "PUT /orgs/acme/patterns/payments":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["title"] != "Payments" {
				t.Errorf("body = %v", body)
			}
			_, _ = w.Write([]byte(`{"id":"org:payments","name":"payments","title":"Payments","updatedAt":9,"templates":{"neutral":"flow"},"slots":[],"notes":{"keep":"","rename":"","split":""}}`))
		case "GET /orgs/acme/patterns/gone", "DELETE /orgs/acme/patterns/gone":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":true,"errorCode":"pattern.not_found","message":"No such pattern"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv, "k")
	_, err := c.CreatePattern(context.Background(), "acme", json.RawMessage(`{"name":"payments"}`))
	if !HasCode(err, "orgs.pattern_exists") {
		t.Errorf("CreatePattern err = %v, want orgs.pattern_exists", err)
	}
	got, err := c.UpdatePattern(context.Background(), "acme", "payments", json.RawMessage(`{"title":"Payments"}`))
	if err != nil || got.ID != "org:payments" || got.UpdatedAt != 9 || got.Templates["neutral"] != "flow" {
		t.Errorf("UpdatePattern = %+v, %v", got, err)
	}
	if _, err := c.GetPattern(context.Background(), "acme", "gone"); !IsNotFound(err) {
		t.Errorf("GetPattern(gone) err = %v, want a 404", err)
	}
	var apiErr *APIError
	if err := c.DeletePattern(context.Background(), "acme", "gone"); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("DeletePattern(gone) err = %v, want a 404", err)
	}
}
