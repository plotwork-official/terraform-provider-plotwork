// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maxIconBytes is the largest icon file the API takes: 32 KB.
	maxIconBytes = 32 << 10
	// sourceFetchTimeout bounds one download of an icon's source_url.
	sourceFetchTimeout = 30 * time.Second
	// maxSourceRedirects is how many redirects a download follows.
	maxSourceRedirects = 5
)

// sourceHTTPClient downloads source_url files. It follows only https
// redirects. Tests replace it with a client that trusts their server.
var sourceHTTPClient = &http.Client{
	Timeout: sourceFetchTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("a redirect to %s://%s left https", req.URL.Scheme, req.URL.Host)
		}
		if len(via) >= maxSourceRedirects {
			return fmt.Errorf("more than %d redirects", maxSourceRedirects)
		}
		return nil
	},
}

// svgContentTypes are the Content-Type values an SVG is served with. Object
// stores often serve one as octet-stream or plain text when nobody set the
// type, so those are taken too, and the body must look like an SVG.
var svgContentTypes = []string{
	"image/svg+xml", "application/xml", "text/xml", "text/plain",
	"application/octet-stream", "binary/octet-stream",
}

// errNotHTTPS refuses a source_url that is not https.
var errNotHTTPS = errors.New("source_url must be an https:// address")

// fetchIconSource downloads the SVG at an https address, at most 32 KB.
// The address is never logged with its query string, which may hold a
// signature such as an Azure SAS token.
func fetchIconSource(ctx context.Context, address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", errNotHTTPS
	}
	shown := u.Redacted()
	if u.RawQuery != "" {
		shown = strings.SplitN(shown, "?", 2)[0] + "?<query hidden>"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("building the request for %s: %w", shown, err)
	}
	req.Header.Set("Accept", "image/svg+xml")
	resp, err := sourceHTTPClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return "", fmt.Errorf("downloading %s: %w", shown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered HTTP %d", shown, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || !isSVGContentType(mediaType) {
			return "", fmt.Errorf("%s is served as %q, not as an SVG (image/svg+xml)", shown, ct)
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIconBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", shown, err)
	}
	if len(body) > maxIconBytes {
		return "", fmt.Errorf("%s is larger than 32 KB, the most an icon may be", shown)
	}
	if !utf8.Valid(body) || !strings.Contains(strings.ToLower(string(body)), "<svg") {
		return "", fmt.Errorf("%s is not an SVG file", shown)
	}
	return string(body), nil
}

// isSVGContentType reports whether a media type may carry an SVG.
func isSVGContentType(mediaType string) bool {
	for _, t := range svgContentTypes {
		if strings.EqualFold(mediaType, t) {
			return true
		}
	}
	return false
}

// sha256Hex is the hex SHA-256 of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
