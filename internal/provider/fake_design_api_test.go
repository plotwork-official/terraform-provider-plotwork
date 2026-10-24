// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

// The fake API's organisation design routes: brand, icons and patterns,
// with the real API's codes and shapes. Where the real check is subtler,
// the fake refuses one simple, documented case so each 422 path can be
// driven from Terraform.

// designOrg is the organisation the design tests work in.
const designOrg = "acme"

// Colours the fake makes from the primary when none was chosen.
const (
	fakeDerivedSecondary = "#7c3aed"
	fakeDerivedAccent    = "#0ea5a5"
)

// fakeBrand is a saved brand: the colours a person chose, normalised.
type fakeBrand struct {
	primary, secondary, accent string
}

// fakeIcon is a saved icon.
type fakeIcon struct {
	name      string
	svg       string
	aliases   []string
	updatedAt int64
}

// fakePattern is a saved pattern: the file's fields without name and
// $schema, as the real API keeps them.
type fakePattern struct {
	name      string
	fields    map[string]any
	updatedAt int64
}

// mountDesign adds the design routes to mux.
func (f *fakeAPI) mountDesign(mux *http.ServeMux) {
	mux.HandleFunc("GET /orgs/{org}/brand", f.authed(f.handleGetBrand))
	mux.HandleFunc("PUT /orgs/{org}/brand", f.designer(f.handlePutBrand))
	mux.HandleFunc("DELETE /orgs/{org}/brand", f.designer(f.handleDeleteBrand))
	mux.HandleFunc("GET /orgs/{org}/icons", f.authed(f.handleListIcons))
	mux.HandleFunc("PUT /orgs/{org}/icons/{name}", f.designer(f.handlePutIcon))
	mux.HandleFunc("DELETE /orgs/{org}/icons/{name}", f.designer(f.handleDeleteIcon))
	mux.HandleFunc("GET /orgs/{org}/patterns/{name}", f.authed(f.handleGetPattern))
	mux.HandleFunc("POST /orgs/{org}/patterns", f.designer(f.handlePostPattern))
	mux.HandleFunc("PUT /orgs/{org}/patterns/{name}", f.designer(f.handlePutPattern))
	mux.HandleFunc("DELETE /orgs/{org}/patterns/{name}", f.designer(f.handleDeletePattern))
}

// designer refuses a key without the design:write scope, as the API does.
func (f *fakeAPI) designer(next http.HandlerFunc) http.HandlerFunc {
	return f.authed(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") == fakeNoDesignKey {
			writeError(w, http.StatusForbidden, "orgs.forbidden", "This key cannot change the organisation's design")
			return
		}
		next(w, r)
	})
}

// tick is a later updatedAt than any before. The caller holds f.mu.
func (f *fakeAPI) tick() int64 {
	f.clock += 1000
	return f.clock
}

// answer is a saved brand as the API answers it, every colour written out,
// and the colours a person chose.
func (b *fakeBrand) answer() (map[string]string, []string) {
	colours := map[string]string{"primary": b.primary, "secondary": fakeDerivedSecondary, "accent": fakeDerivedAccent}
	chosen := []string{"primary"}
	if b.secondary != "" {
		colours["secondary"], chosen = b.secondary, append(chosen, "secondary")
	}
	if b.accent != "" {
		colours["accent"], chosen = b.accent, append(chosen, "accent")
	}
	return colours, chosen
}

func (f *fakeAPI) handleGetBrand(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.brands[r.PathValue("org")]
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"brand": nil, "chosen": []string{}, "active": false, "canEdit": true, "plan": "pro"})
		return
	}
	colours, chosen := b.answer()
	writeJSON(w, http.StatusOK, map[string]any{"brand": colours, "chosen": chosen, "active": true, "canEdit": true, "plan": "pro"})
}

// handlePutBrand saves a brand. Besides a colour that is not one, the fake
// refuses a secondary or accent equal to the primary, to drive the 422
// path (the real check calls that a warning). A primary of #dc2626 gets the
// real check's reads-as-status warning.
func (f *fakeAPI) handlePutBrand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Primary   *string `json:"primary"`
		Secondary *string `json:"secondary"`
		Accent    *string `json:"accent"`
	}
	f.decode(r, &body)

	var issues []map[string]string
	read := func(role string, v *string) string {
		if v == nil {
			return ""
		}
		c, ok := normaliseColour.normalise(*v)
		if !ok {
			//nolint:misspell // The API's issue code is spelt the American way.
			issues = append(issues, map[string]string{"role": role, "severity": "error", "code": "not-a-color", "message": *v + " is not a colour"})
		}
		return c
	}
	b := &fakeBrand{primary: read("primary", body.Primary), secondary: read("secondary", body.Secondary), accent: read("accent", body.Accent)}
	if body.Primary == nil {
		issues = append(issues, map[string]string{"role": "primary", "severity": "error", "code": "missing-primary", "message": "A brand needs a primary colour"})
	}
	for role, c := range map[string]string{"secondary": b.secondary, "accent": b.accent} {
		if c != "" && c == b.primary {
			issues = append(issues, map[string]string{"role": role, "severity": "error", "code": "too-similar", "message": "The " + role + " is the primary"})
		}
	}
	if len(issues) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": true, "errorCode": "brand.invalid", "message": "The brand colours are not valid.", "details": issues,
		})
		return
	}
	warnings := []map[string]string{}
	if b.primary == "#dc2626" {
		warnings = append(warnings, map[string]string{"role": "primary", "severity": "warning", "code": "reads-as-status", "message": "Reads as an error colour"})
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.brands[r.PathValue("org")] = b
	colours, chosen := b.answer()
	writeJSON(w, http.StatusOK, map[string]any{"brand": colours, "chosen": chosen, "check": map[string]any{"issues": warnings, "swatches": nil}})
}

func (f *fakeAPI) handleDeleteBrand(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.brands, r.PathValue("org"))
	w.WriteHeader(http.StatusNoContent)
}

// iconAnswer is an icon as the list and a save answer it.
func iconAnswer(i *fakeIcon) map[string]any {
	return map[string]any{
		"name": i.name, "ref": "org:" + i.name, "aliases": i.aliases, "mediaType": "image/svg+xml",
		"image": "data:image/svg+xml;base64,", "width": 24, "height": 24, "bytes": len(i.svg), "updatedAt": i.updatedAt,
	}
}

func (f *fakeAPI) handleListIcons(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	icons := []map[string]any{}
	for _, i := range f.icons[r.PathValue("org")] {
		icons = append(icons, iconAnswer(i))
	}
	writeJSON(w, http.StatusOK, map[string]any{"icons": icons, "active": true, "canEdit": true, "plan": "pro", "limit": 100})
}

// handlePutIcon saves an icon. The fake refuses an SVG with a script, as
// the real check refuses anything outside its allowlist, and reports a
// <metadata> element as removed.
func (f *fakeAPI) handlePutIcon(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SVG     *string  `json:"svg"`
		PNG     *string  `json:"png"`
		Aliases []string `json:"aliases"`
	}
	f.decode(r, &body)
	name := r.PathValue("name")

	var issues []map[string]string
	if !iconNamePattern.MatchString(name) {
		issues = append(issues, map[string]string{"code": "bad-name", "message": name + " is not a name"})
	}
	if body.SVG != nil && strings.Contains(*body.SVG, "<script") {
		issues = append(issues, map[string]string{"code": "unsafe", "message": "The SVG has a script", "hint": "Export it again without scripts"})
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	org := r.PathValue("org")
	existing := f.icons[org][name]
	if body.SVG == nil && body.PNG == nil && existing == nil {
		issues = append(issues, map[string]string{"code": "empty", "message": "There is no icon named " + name + " yet, so it needs a file."})
	}
	if len(issues) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": true, "errorCode": "icon.invalid", "message": "The icon cannot be saved.", "details": issues,
		})
		return
	}

	if f.icons[org] == nil {
		f.icons[org] = map[string]*fakeIcon{}
	}
	icon := &fakeIcon{name: name}
	if existing != nil {
		copied := *existing
		icon = &copied
	}
	removed := []string{}
	if body.SVG != nil {
		icon.svg = *body.SVG
		if strings.Contains(icon.svg, "<metadata") {
			removed = append(removed, "metadata")
		}
	}
	icon.aliases = []string{}
	for _, a := range body.Aliases {
		a = strings.Join(strings.Fields(a), " ")
		if !slices.Contains(icon.aliases, a) {
			icon.aliases = append(icon.aliases, a)
		}
	}
	icon.updatedAt = f.tick()
	f.icons[org][name] = icon
	writeJSON(w, http.StatusOK, map[string]any{"icon": iconAnswer(icon), "removed": removed})
}

func (f *fakeAPI) handleDeleteIcon(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.icons[r.PathValue("org")], r.PathValue("name"))
	w.WriteHeader(http.StatusNoContent)
}

// patternAnswer is a pattern as the API answers it, with the defaults the
// real API fills in.
func patternAnswer(p *fakePattern) map[string]any {
	out := map[string]any{
		"id": "org:" + p.name, "name": p.name, "kind": "flow", "whenToUse": "", "tags": []string{}, "slots": []any{},
		"notes": map[string]string{"keep": "", "rename": "", "split": ""}, "createdAt": p.updatedAt, "updatedAt": p.updatedAt,
		"updatedBy": nil, "pictures": nil, "slotCount": 0,
	}
	for k, v := range p.fields {
		out[k] = v
	}
	flavours := []string{}
	if templates, ok := p.fields["templates"].(map[string]any); ok {
		for flavour := range templates {
			flavours = append(flavours, flavour)
		}
	}
	slices.Sort(flavours)
	out["flavours"] = flavours
	return out
}

// patternProblems is the fake's check of a pattern: a title, a summary and
// at least one template, none of which says "broken".
func patternProblems(fields map[string]any) []map[string]string {
	var problems []map[string]string
	for _, field := range []string{"title", "summary"} {
		if s, ok := fields[field].(string); !ok || s == "" {
			problems = append(problems, map[string]string{"field": field, "message": "A pattern needs a " + field})
		}
	}
	templates, _ := fields["templates"].(map[string]any)
	if len(templates) == 0 {
		problems = append(problems, map[string]string{"field": "templates", "message": "A pattern needs at least one template"})
	}
	for flavour, source := range templates {
		if s, _ := source.(string); strings.Contains(s, "broken") {
			problems = append(problems, map[string]string{"field": "templates." + flavour, "message": "E101 2:1 Unknown statement"})
		}
	}
	return problems
}

// writePatternInvalid answers 422 orgs.pattern_invalid.
func writePatternInvalid(w http.ResponseWriter, problems []map[string]string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"error": true, "errorCode": "orgs.pattern_invalid", "message": "The pattern has problems to fix before it can be saved.",
		"details": map[string]any{"problems": problems},
	})
}

func (f *fakeAPI) handleGetPattern(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.patterns[r.PathValue("org")][r.PathValue("name")]
	if !ok {
		writeError(w, http.StatusNotFound, "pattern.not_found", "No such pattern")
		return
	}
	writeJSON(w, http.StatusOK, patternAnswer(p))
}

func (f *fakeAPI) handlePostPattern(w http.ResponseWriter, r *http.Request) {
	var fields map[string]any
	f.decode(r, &fields)
	name, _ := fields["name"].(string)
	delete(fields, "name")
	delete(fields, "$schema")
	if problems := patternProblems(fields); len(problems) > 0 {
		writePatternInvalid(w, problems)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	org := r.PathValue("org")
	if _, taken := f.patterns[org][name]; taken {
		writeError(w, http.StatusConflict, "orgs.pattern_exists", "The organisation already has a pattern named org:"+name)
		return
	}
	if f.patterns[org] == nil {
		f.patterns[org] = map[string]*fakePattern{}
	}
	p := &fakePattern{name: name, fields: fields, updatedAt: f.tick()}
	f.patterns[org][name] = p
	writeJSON(w, http.StatusCreated, patternAnswer(p))
}

func (f *fakeAPI) handlePutPattern(w http.ResponseWriter, r *http.Request) {
	var fields map[string]any
	f.decode(r, &fields)
	if _, ok := fields["name"]; ok {
		writeError(w, http.StatusBadRequest, "validation.failed", "name is not allowed")
		return
	}
	delete(fields, "$schema")
	if problems := patternProblems(fields); len(problems) > 0 {
		writePatternInvalid(w, problems)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.patterns[r.PathValue("org")][r.PathValue("name")]
	if !ok {
		writeError(w, http.StatusNotFound, "pattern.not_found", "No such pattern")
		return
	}
	p.fields, p.updatedAt = fields, f.tick()
	writeJSON(w, http.StatusOK, patternAnswer(p))
}

func (f *fakeAPI) handleDeletePattern(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	org, name := r.PathValue("org"), r.PathValue("name")
	if _, ok := f.patterns[org][name]; !ok {
		writeError(w, http.StatusNotFound, "pattern.not_found", "No such pattern")
		return
	}
	delete(f.patterns[org], name)
	w.WriteHeader(http.StatusNoContent)
}

// patternFieldsHeld is a saved pattern's fields as JSON, for assertions.
func (f *fakeAPI) patternFieldsHeld(name string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.patterns[designOrg][name]
	if !ok {
		return "", false
	}
	out, _ := json.Marshal(p.fields)
	return string(out), true
}

// paymentsIconHeld is the saved payments-api icon, for assertions.
func (f *fakeAPI) paymentsIconHeld() (fakeIcon, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.icons[designOrg]["payments-api"]
	if !ok {
		return fakeIcon{}, false
	}
	return *i, true
}

// brandHeld is a saved brand, for assertions.
func (f *fakeAPI) brandHeld() (fakeBrand, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.brands[designOrg]
	if !ok {
		return fakeBrand{}, false
	}
	return *b, true
}

// touchIcon changes an icon as someone in the portal would.
func (f *fakeAPI) touchIcon(name, svg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i, ok := f.icons[designOrg][name]; ok {
		i.svg, i.updatedAt = svg, f.tick()
	}
}

// touchPattern changes a pattern's title as someone in the portal would.
func (f *fakeAPI) touchPattern(name, title string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.patterns[designOrg][name]; ok {
		p.fields["title"], p.updatedAt = title, f.tick()
	}
}
