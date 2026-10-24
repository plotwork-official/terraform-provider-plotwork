// Copyright (c) 2026 Buun Group
// SPDX-License-Identifier: MIT

// Package client is a small HTTP client for the Plotwork REST API.
//
// It covers only the routes the Terraform provider needs: importing a file
// into Plotwork source, rendering source to SVG, and publishing and deleting
// hosted diagrams. Every failing response is returned as an *APIError that
// carries the API's stable error code, so callers branch on codes, not on
// message text.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the hosted Plotwork API.
const DefaultBaseURL = "https://api.plotwork.dev"

const (
	// requestTimeout bounds one HTTP attempt. A layout of a large diagram
	// runs in the API's engine and can take several seconds.
	requestTimeout = 60 * time.Second

	// maxAttempts is how many times a request is sent when the API answers
	// 429 or a 5xx status, or the connection fails.
	maxAttempts = 4

	// maxBackoff caps the wait between two attempts, including a wait the
	// API asked for with Retry-After.
	maxBackoff = 30 * time.Second

	// maxResponseBytes caps how much of a response body is read. The API
	// caps a stored diagram well below this.
	maxResponseBytes = 32 << 20
)

// Client calls the Plotwork API with one API key.
type Client struct {
	baseURL    string
	apiKey     string
	userAgent  string
	httpClient *http.Client

	// sleep waits between retries. Tests replace it to run without waiting.
	sleep func(ctx context.Context, d time.Duration) error
}

// Option changes how a Client is built.
type Option func(*Client)

// WithHTTPClient sets the HTTP client requests are sent with.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// New returns a Client for the API at baseURL, or DefaultBaseURL when baseURL
// is empty.
func New(baseURL, apiKey string, opts ...Option) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		userAgent:  "terraform-provider-plotwork",
		httpClient: &http.Client{Timeout: requestTimeout},
		sleep:      sleepContext,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// APIError is a failing response from the API, read from its error envelope
// ({"error": true, "errorCode": "...", "message": "..."}).
type APIError struct {
	// StatusCode is the HTTP status of the response.
	StatusCode int
	// Code is the API's stable error code, such as "hosting.diagram_locked".
	// It is empty when the body was not the API's envelope.
	Code string `json:"errorCode"`
	// Message is the API's human-readable summary.
	Message string `json:"message"`
	// Details holds the API's structured details, such as diagnostics.
	Details json.RawMessage `json:"details,omitempty"`
	// RequestID correlates the call with the API's logs.
	RequestID string `json:"requestId,omitempty"`
}

// Error implements error.
func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if e.Code != "" {
		msg = fmt.Sprintf("%s (%s, HTTP %d)", msg, e.Code, e.StatusCode)
	} else {
		msg = fmt.Sprintf("%s (HTTP %d)", msg, e.StatusCode)
	}
	if e.RequestID != "" {
		msg += " request " + e.RequestID
	}
	return msg
}

// IsNotFound reports whether err is an API answer of 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// HasCode reports whether err is an API answer with the given error code.
func HasCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == code
}

// do sends one JSON request and decodes a JSON answer into out, which may be
// nil when the answer has no body. It retries 429, 5xx and connection
// failures with exponential backoff, honouring Retry-After. Every call the
// provider makes is safe to repeat: a publish of the same source returns the
// diagram it already stored, and a delete of a deleted diagram is a 404.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, header, respBody, err := c.send(ctx, method, path, payload)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
		case status >= 200 && status < 300:
			if out == nil || len(respBody) == 0 {
				return nil
			}
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("decoding %s %s answer: %w", method, path, err)
			}
			return nil
		default:
			apiErr := decodeError(status, respBody)
			if !retryable(status) {
				return apiErr
			}
			lastErr = apiErr
		}

		if attempt == maxAttempts {
			break
		}
		if err := c.sleep(ctx, backoff(attempt, header)); err != nil {
			return err
		}
	}
	return lastErr
}

// send makes one HTTP attempt and returns the status, headers and body.
func (c *Client) send(ctx context.Context, method, path string, payload []byte) (int, http.Header, []byte, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("building %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("reading %s %s answer: %w", method, path, err)
	}
	return resp.StatusCode, resp.Header, respBody, nil
}

// decodeError reads the API's error envelope, falling back to the status
// alone when the body is not one (a proxy's HTML page, for example).
func decodeError(status int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status}
	if err := json.Unmarshal(body, apiErr); err != nil {
		apiErr.Code, apiErr.Message = "", ""
	}
	apiErr.StatusCode = status
	return apiErr
}

// retryable reports whether a status is worth sending again.
func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// backoff is the wait before the next attempt: Retry-After when the API sent
// one in seconds, else 1 s, 2 s, 4 s, capped at maxBackoff.
func backoff(attempt int, header http.Header) time.Duration {
	if header != nil {
		if secs, err := strconv.Atoi(header.Get("Retry-After")); err == nil && secs >= 0 {
			return min(time.Duration(secs)*time.Second, maxBackoff)
		}
	}
	return min(time.Second<<(attempt-1), maxBackoff)
}

// sleepContext waits for d, or returns early when ctx ends.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
