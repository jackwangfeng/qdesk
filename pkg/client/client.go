// Package client is the Go SDK for the qdesk control plane HTTP API.
//
// Typical use:
//
//	c := client.New("http://localhost:8080", "sk-...")
//	sess, _ := c.CreateSession(ctx, &client.CreateSessionRequest{
//	    Template: "ubuntu-chrome",
//	    OpenURL:  "https://example.com",
//	})
//	defer c.DeleteSession(ctx, sess.ID)
//	png, _ := c.Screenshot(ctx, sess.ID)
//	_, _ = c.Action(ctx, sess.ID, &protocol.Action{Type: "click", X: 10, Y: 20})
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// Re-export the request type so callers don't have to import protocol just
// to construct a session.
type CreateSessionRequest = protocol.CreateSessionRequest

// Client talks to a qdesk-control HTTP server.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// New returns a Client with sensible defaults.
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// CreateSession spins up a new sandbox.
func (c *Client) CreateSession(ctx context.Context, req *CreateSessionRequest) (*protocol.Session, error) {
	body, _ := json.Marshal(req)
	httpReq, err := c.newRequest(ctx, http.MethodPost, "/v1/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, decodeAPIError(resp)
	}
	var s protocol.Session
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	return &s, nil
}

// DeleteSession tears down a session and its container.
func (c *Client) DeleteSession(ctx context.Context, id string) error {
	httpReq, err := c.newRequest(ctx, http.MethodDelete, "/v1/sessions/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		return decodeAPIError(resp)
	}
	return nil
}

// GetSession fetches the current state of a session.
func (c *Client) GetSession(ctx context.Context, id string) (*protocol.Session, error) {
	httpReq, err := c.newRequest(ctx, http.MethodGet, "/v1/sessions/"+id, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, decodeAPIError(resp)
	}
	var s protocol.Session
	return &s, json.NewDecoder(resp.Body).Decode(&s)
}

// Screenshot returns the current display as PNG bytes.
func (c *Client) Screenshot(ctx context.Context, id string) ([]byte, error) {
	httpReq, err := c.newRequest(ctx, http.MethodGet, "/v1/sessions/"+id+"/screenshot", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, decodeAPIError(resp)
	}
	return io.ReadAll(resp.Body)
}

// Action dispatches one input action.
func (c *Client) Action(ctx context.Context, id string, a *protocol.Action) (*protocol.ActionResult, error) {
	body, _ := json.Marshal(a)
	httpReq, err := c.newRequest(ctx, http.MethodPost, "/v1/sessions/"+id+"/actions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, decodeAPIError(resp)
	}
	var ar protocol.ActionResult
	return &ar, json.NewDecoder(resp.Body).Decode(&ar)
}

// Health pings /v1/health (unauthenticated).
func (c *Client) Health(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	return req, nil
}

// APIError is returned for non-2xx responses with a JSON {error: ...} body.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("qdesk-control %d: %s", e.StatusCode, e.Message)
}

func decodeAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Error == "" {
		env.Error = strings.TrimSpace(string(body))
	}
	return &APIError{StatusCode: resp.StatusCode, Message: env.Error}
}
