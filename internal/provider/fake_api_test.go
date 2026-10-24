// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAPIKey is the only key the fake API accepts.
const fakeAPIKey = "pw_test_key"

// fakeDiagram is one diagram the fake API holds.
type fakeDiagram struct {
	id, library, source, name, folder, alias string
}

// fakeAPI is an in-memory Plotwork API with the routes the provider calls.
// It stores diagrams by content like the real one: the same source in the
// same library is the same diagram.
type fakeAPI struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	diagrams map[string]*fakeDiagram
	deleted  []string
	// routeTo, when set, sends personal publishes to this organisation's
	// library, as an organisation's "required" Publishing setting does.
	routeTo string
}

// newFakeAPI starts a fake API that stops when the test ends.
func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{t: t, diagrams: map[string]*fakeDiagram{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/import", f.authed(f.handleImport))
	mux.HandleFunc("POST /v1/diagrams/render", f.authed(f.handleRender))
	mux.HandleFunc("POST /v1/diagrams/publish", f.authed(f.handlePublish))
	mux.HandleFunc("POST /orgs/{org}/diagrams", f.authed(f.handleOrgPublish))
	mux.HandleFunc("DELETE /user/diagrams/{id}", f.authed(f.handleDelete))
	mux.HandleFunc("DELETE /orgs/{org}/diagrams/{id}", f.authed(f.handleDelete))
	mux.HandleFunc("GET /orgs/{org}/diagrams/{id}/svg", f.authed(f.handleOrgSVG))
	mux.HandleFunc("HEAD /d/{file}", f.handleCDN)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// providerBlock is a provider block pointing at the fake API.
func (f *fakeAPI) providerBlock(extra string) string {
	return `provider "plotwork" {
  api_url = "` + f.srv.URL + `"
  api_key = "` + fakeAPIKey + `"
  ` + extra + `
}
`
}

// held reports the ids the fake API holds, for assertions.
func (f *fakeAPI) held() map[string]*fakeDiagram {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]*fakeDiagram, len(f.diagrams))
	for id, d := range f.diagrams {
		copied := *d
		out[id] = &copied
	}
	return out
}

// forget removes a diagram as the portal or a retention sweep would.
func (f *fakeAPI) forget(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.diagrams, id)
}

func (f *fakeAPI) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != fakeAPIKey {
			writeError(w, http.StatusUnauthorized, "auth.invalid_credentials", "That API key is not valid")
			return
		}
		next(w, r)
	}
}

func (f *fakeAPI) handleImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Format string `json:"format"`
		Text   string `json:"text"`
		Files  []struct {
			Path string `json:"path"`
			Text string `json:"text"`
		} `json:"files"`
	}
	f.decode(r, &body)
	if strings.Contains(body.Text, "unreadable") {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "kind": nil, "source": "", "partsTotal": 0, "outline": []string{}, "coverage": nil, "validation": nil,
			"diagnostics": []map[string]any{{"code": "I101", "severity": "error", "message": "The file could not be read", "line": 1, "column": 1, "length": 1, "hint": "Check the format"}},
		})
		return
	}
	names := make([]string, 0, len(body.Files))
	for _, file := range body.Files {
		names = append(names, file.Path)
	}
	source := "flow\n# " + body.Format + ": " + strings.Join(names, ",") + body.Text
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "kind": "flow", "source": source, "partsTotal": 0, "outline": []string{"Box A", "A -> B"},
		"diagnostics": []map[string]any{{"code": "I201", "severity": "warning", "message": "A count was left out", "line": 3, "column": 2, "length": 1, "hint": nil}},
		"coverage":    map[string]any{"complete": false, "line": "Drawn 2 of 3 resources."},
		"validation":  map[string]any{"valid": true, "errors": []any{}, "warnings": []any{}, "summary": "A flow of 2 boxes", "size": map[string]int{"width": 100, "height": 50}},
	})
}

func (f *fakeAPI) handleRender(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
		Scheme string `json:"scheme"`
	}
	f.decode(r, &body)
	if strings.Contains(body.Source, "broken") {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": true, "errorCode": "diagram.invalid", "message": "The diagram has 1 error",
			"details": []map[string]any{{"code": "E101", "severity": "error", "message": "Unknown statement", "line": 2, "column": 1, "length": 6, "hint": "Write a box"}},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"svg": "<svg data-scheme=\"" + body.Scheme + "\">" + body.Source + "</svg>", "scheme": body.Scheme,
		"size": map[string]int{"width": 640, "height": 480}, "warnings": []any{},
	})
}

// publishBody is a publish's body as the fake reads it.
type publishBody struct {
	Source string  `json:"source"`
	Scheme string  `json:"scheme"`
	Name   string  `json:"name"`
	Folder *string `json:"folder"`
	Alias  string  `json:"alias"`
}

func (f *fakeAPI) handlePublish(w http.ResponseWriter, r *http.Request) {
	var body publishBody
	f.decode(r, &body)
	if f.routeTo != "" {
		d, created := f.store("org-id-"+f.routeTo, body)
		answer := f.answer(d, created)
		answer["private"] = true
		answer["organization"] = map[string]string{"id": "org-id-" + f.routeTo, "slug": f.routeTo, "name": "Routed"}
		writeJSON(w, http.StatusCreated, answer)
		return
	}
	d, created := f.store("personal", body)
	writeJSON(w, http.StatusCreated, f.answer(d, created))
}

func (f *fakeAPI) handleOrgPublish(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	f.decode(r, &raw)
	for _, field := range []string{"alias", "organization", "personal"} {
		if _, ok := raw[field]; ok {
			writeError(w, http.StatusBadRequest, "validation.failed", field+" is not allowed")
			return
		}
	}
	encoded, _ := json.Marshal(raw)
	var body publishBody
	_ = json.Unmarshal(encoded, &body)
	d, created := f.store(r.PathValue("org"), body)
	writeJSON(w, http.StatusCreated, f.answer(d, created))
}

func (f *fakeAPI) handleDelete(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	library := "personal"
	if org := r.PathValue("org"); org != "" {
		library = org
	}
	d, ok := f.diagrams[r.PathValue("id")]
	if !ok || d.library != library {
		writeError(w, http.StatusNotFound, "hosting.not_found", "No such diagram")
		return
	}
	delete(f.diagrams, d.id)
	f.deleted = append(f.deleted, d.id)
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeAPI) handleOrgSVG(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.diagrams[r.PathValue("id")]; !ok || d.library != r.PathValue("org") {
		writeError(w, http.StatusNotFound, "orgs.not_found", "No such diagram")
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = w.Write([]byte("<svg/>"))
}

func (f *fakeAPI) handleCDN(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.diagrams[strings.TrimSuffix(r.PathValue("file"), ".svg")]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// store keeps a diagram by its content and library, and reports whether it
// is new. Publishing it again renames and moves it, as the real API does.
func (f *fakeAPI) store(library string, body publishBody) (*fakeDiagram, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256([]byte(library + "\x00" + body.Scheme + "\x00" + body.Source))
	id := hex.EncodeToString(sum[:8])
	d, ok := f.diagrams[id]
	if !ok {
		d = &fakeDiagram{id: id, library: library, source: body.Source}
		f.diagrams[id] = d
	}
	if body.Name != "" {
		d.name = body.Name
	}
	d.folder = ""
	if body.Folder != nil {
		d.folder = *body.Folder
	}
	d.alias = body.Alias
	return d, !ok
}

// answer is the publish answer for a diagram.
func (f *fakeAPI) answer(d *fakeDiagram, created bool) map[string]any {
	out := map[string]any{
		"id": d.id, "scheme": "light", "bytes": len(d.source) + 100, "publishedAt": 1_760_000_000_000,
		"expiresAt": nil, "locked": false, "created": created, "name": nil, "folder": nil,
	}
	if d.library == "personal" {
		out["url"] = f.srv.URL + "/d/" + d.id + ".svg"
		out["expiresAt"] = 1_762_592_000_000
	} else {
		out["viewUrl"] = "https://app.plotwork.dev/orgs/" + d.library + "/diagrams/" + d.id
	}
	if d.alias != "" {
		out["stableLink"] = map[string]string{"id": "sl-" + d.alias, "alias": d.alias, "url": f.srv.URL + "/l/" + d.alias + ".svg"}
	}
	return out
}

func (f *fakeAPI) decode(r *http.Request, into any) {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		f.t.Errorf("fake API: decoding %s %s: %v", r.Method, r.URL.Path, err)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": true, "errorCode": code, "message": message})
}
