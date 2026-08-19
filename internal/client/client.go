// Package client is a thin HTTP client for the Basa /api/v1 surface.
//
// It knows how to send a bearer token and how to turn an HTTP status into the
// CLI's error contract. It holds no domain logic — every response is handed
// back as decoded JSON for a command to render.
package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Basa-Futura/basa-cli/internal/fail"
)

// Client talks to one Basa environment.
type Client struct {
	baseURL string
	token   string
	env     string
	http    *http.Client
}

func New(env, baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		env:     env,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Me fetches the authenticated caller.
func (c *Client) Me(ctx context.Context) (*Me, error) {
	var envelope struct {
		Data Me `json:"data"`
	}
	if err := c.get(ctx, "/api/v1/me", &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

// MeRaw fetches the caller as undecoded JSON, so --json can emit the API's own
// shape rather than a re-serialized approximation of it.
func (c *Client) MeRaw(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/api/v1/me", &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Me mirrors the fields of GET /api/v1/me that the CLI renders.
type Me struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Teams []Team `json:"teams"`
	Token struct {
		Name      string   `json:"name"`
		Abilities []string `json:"abilities"`
		ExpiresAt *string  `json:"expires_at"`
	} `json:"token"`
}

type Team struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Personal bool   `json:"personal"`
}

func (c *Client) get(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fail.Usagef("Could not build the request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fail.Unreachable(c.env, c.baseURL)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if err := c.statusError(resp.StatusCode, body); err != nil {
		return err
	}

	if into == nil {
		return nil
	}

	if err := json.Unmarshal(body, into); err != nil {
		return fail.Wrap(fail.CodeUsage, "The server sent a response this version of basa could not read.", err)
	}

	return nil
}

// statusError maps HTTP status onto the CLI's exit-code contract. Each branch
// tells the operator what to do, not what went wrong internally.
func (c *Client) statusError(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}

	switch status {
	case http.StatusUnauthorized:
		return fail.TokenRejected(c.env)

	case http.StatusForbidden:
		// 403 is either "API access is off for this account" or "this token
		// lacks the ability". The server's own message distinguishes them, so
		// prefer it over anything invented here.
		msg, hint := apiMessage(body)
		if msg == "" {
			msg = "You do not have access to that."
		}
		if hint == "" {
			hint = "Ask a Basa administrator to enable API access for you."
		}
		return fail.Forbidden(msg, hint)

	case http.StatusNotFound:
		return fail.NotFound("That does not exist, or you cannot see it.")

	case http.StatusTooManyRequests:
		return fail.RateLimited()

	case http.StatusUnprocessableEntity:
		msg, _ := apiMessage(body)
		if msg == "" {
			msg = "The request was rejected as invalid."
		}
		return fail.Usage(msg)
	}

	if status >= 500 {
		return fail.ServerError(c.env, status)
	}

	return fail.Usagef("Unexpected response from the server (%d).", status)
}

// apiMessage pulls Laravel's conventional `message` field, plus our `hint`.
func apiMessage(body []byte) (msg, hint string) {
	var payload struct {
		Message string `json:"message"`
		Hint    string `json:"hint"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", ""
	}
	return payload.Message, payload.Hint
}
