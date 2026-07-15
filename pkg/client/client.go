// Package client is the typed Go SDK over Glyphux's HTTP content API — the
// second client of the composition contract (after the raw HTTP API; PRD
// §3.3, slice 1.11). It holds no privileged access of its own: every call
// goes over the same public /api/v0 surface any other client uses.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client is a typed client for one Glyphux daemon. It is safe for
// concurrent use once authenticated; Login/SetToken mutate shared state and
// should complete before concurrent requests begin.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string

	Auth    *AuthService
	Content *ContentService
	Media   *MediaService
	Users   *UsersService
}

// New builds a Client against baseURL (e.g. "http://localhost:8080"), with
// no credentials — call Auth.Login or SetToken before making authenticated
// requests.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.Auth = &AuthService{c: c}
	c.Content = &ContentService{c: c}
	c.Media = &MediaService{c: c}
	c.Users = &UsersService{c: c}
	return c
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client (e.g. for custom
// timeouts or transports).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithToken pre-authenticates the Client with an already-issued bearer
// token, skipping Auth.Login.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// SetToken sets or clears (with "") the bearer token used for subsequent
// requests.
func (c *Client) SetToken(token string) { c.token = token }

// APIError is returned for any non-2xx response. It carries the HTTP status
// and, when the server reported field-level validation problems, the raw
// issues payload.
type APIError struct {
	StatusCode int
	Message    string
	Issues     json.RawMessage
}

func (e *APIError) Error() string {
	return fmt.Sprintf("glyphux: %s (status %d)", e.Message, e.StatusCode)
}

// AsAPIError reports whether err is (or wraps) an *APIError, writing it into
// target on success — a typed convenience over errors.As for SDK callers.
func AsAPIError(err error, target **APIError) bool {
	return errors.As(err, target)
}

// doJSON performs an HTTP request with an optional JSON-encoded body,
// decoding a JSON response into out (if non-nil). A non-2xx response is
// translated into an *APIError.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("glyphux: encode request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("glyphux: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("glyphux: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("glyphux: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(resp.StatusCode, respBody)
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("glyphux: decode response: %w", err)
	}
	return nil
}

func newAPIError(status int, body []byte) *APIError {
	var payload struct {
		Error  string          `json:"error"`
		Issues json.RawMessage `json:"issues"`
	}
	msg := string(body)
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		msg = payload.Error
	}
	return &APIError{StatusCode: status, Message: msg, Issues: payload.Issues}
}
