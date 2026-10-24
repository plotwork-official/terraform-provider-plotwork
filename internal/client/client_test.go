// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a client for srv that never sleeps between retries.
func newTestClient(srv *httptest.Server, key string) *Client {
	c := New(srv.URL, key, WithHTTPClient(srv.Client()), WithUserAgent("test-agent"))
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestRenderSendsKeyAndDecodesAnswer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "pw_test" {
			t.Errorf("X-API-Key = %q, want pw_test", got)
		}
		if got := r.Header.Get("User-Agent"); got != "test-agent" {
			t.Errorf("User-Agent = %q, want test-agent", got)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/diagrams/render" {
			t.Errorf("request = %s %s, want POST /v1/diagrams/render", r.Method, r.URL.Path)
		}
		var body RenderRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Source != "flow" {
			t.Errorf("body = %+v (%v), want source flow", body, err)
		}
		_, _ = w.Write([]byte(`{"svg":"<svg/>","scheme":"light","size":{"width":10,"height":20},"warnings":[]}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv, "pw_test").Render(context.Background(), RenderRequest{Source: "flow"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got.SVG != "<svg/>" || got.Size.Width != 10 || got.Size.Height != 20 {
		t.Errorf("Render = %+v", got)
	}
}

func TestErrorEnvelopeIsDecoded(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":true,"errorCode":"hosting.diagram_locked","message":"The diagram is locked","requestId":"r1"}`))
	}))
	defer srv.Close()

	err := newTestClient(srv, "k").DeleteDiagram(context.Background(), "abc")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusConflict || apiErr.Code != "hosting.diagram_locked" || apiErr.RequestID != "r1" {
		t.Errorf("APIError = %+v", apiErr)
	}
	if !HasCode(err, "hosting.diagram_locked") || IsNotFound(err) {
		t.Errorf("HasCode/IsNotFound disagree with %v", err)
	}
}

func TestBodyThatIsNotTheEnvelopeKeepsTheStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<html>not found</html>`))
	}))
	defer srv.Close()

	err := newTestClient(srv, "k").DeleteDiagram(context.Background(), "abc")
	if !IsNotFound(err) {
		t.Fatalf("err = %v, want a 404 APIError", err)
	}
}

func TestRetriesTooManyRequestsAndServerErrors(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv, "k")
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	if err := c.DeleteDiagram(context.Background(), "abc"); err != nil {
		t.Fatalf("DeleteDiagram: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
	if len(waits) != 2 || waits[0] != 2*time.Second || waits[1] != 2*time.Second {
		t.Errorf("waits = %v, want [2s 2s] (Retry-After, then the second backoff step)", waits)
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":true,"errorCode":"diagram.invalid","message":"bad","details":[{"code":"E101","severity":"error","message":"Unknown statement","line":2,"column":1,"length":3,"hint":null}]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv, "k").Render(context.Background(), RenderRequest{Source: "x"})
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || len(apiErr.Details) == 0 {
		t.Fatalf("err = %v, want an APIError with details", err)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := newTestClient(srv, "k").DeleteDiagram(context.Background(), "abc")
	if calls.Load() != maxAttempts {
		t.Errorf("calls = %d, want %d", calls.Load(), maxAttempts)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("err = %v, want the last 503", err)
	}
}

func TestPublicDiagramExistsSendsNoKey(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "" {
			t.Error("the CDN request carried the API key")
		}
		if r.URL.Path == "/d/gone.svg" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv, "pw_secret")
	if ok, err := c.PublicDiagramExists(context.Background(), srv.URL+"/d/here.svg"); err != nil || !ok {
		t.Errorf("here = %v, %v; want true", ok, err)
	}
	if ok, err := c.PublicDiagramExists(context.Background(), srv.URL+"/d/gone.svg"); err != nil || ok {
		t.Errorf("gone = %v, %v; want false", ok, err)
	}
}

func TestPublishToOrganizationDropsPersonalFields(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/orgs/the%20team/diagrams" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, field := range []string{"alias", "organization", "personal"} {
			if _, ok := body[field]; ok {
				t.Errorf("body has %s, which the organisation route refuses", field)
			}
		}
		if folder, ok := body["folder"]; !ok || folder != nil {
			t.Errorf("folder = %v, %v; want an explicit null", folder, ok)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"d1","viewUrl":"https://app.plotwork.dev/x","scheme":"light","bytes":3,"publishedAt":0,"expiresAt":null,"locked":false,"created":true,"name":null,"folder":null}`))
	}))
	defer srv.Close()

	got, err := newTestClient(srv, "k").PublishToOrganization(context.Background(), "the team",
		PublishRequest{Source: "s", Alias: "a", Organization: "o", Personal: true})
	if err != nil || got.ID != "d1" || got.ViewURL == "" {
		t.Fatalf("PublishToOrganization = %+v, %v", got, err)
	}
}

func TestBackoffIsCapped(t *testing.T) {
	t.Parallel()
	if got := backoff(10, nil); got != maxBackoff {
		t.Errorf("backoff(10) = %v, want %v", got, maxBackoff)
	}
	h := http.Header{}
	h.Set("Retry-After", "3600")
	if got := backoff(1, h); got != maxBackoff {
		t.Errorf("backoff with Retry-After 3600 = %v, want %v", got, maxBackoff)
	}
}
