package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/terraprovider/go-msadmin/httpx"

	"github.com/terraprovider/terraform-provider-teams/internal/clients"
)

const (
	graphBaseURL  = "https://graph.microsoft.com/v1.0"
	graphResource = "https://graph.microsoft.com"
	graphBodyMax  = 600
)

// graphDo sends one Microsoft Graph request and returns the response body. A
// non-2xx response is returned as an *httpx.APIError.
func graphDo(ctx context.Context, c *clients.Client, method, path string, body any) ([]byte, error) {
	if c == nil || c.Tokens == nil {
		return nil, fmt.Errorf("microsoft graph: the provider has no token source")
	}
	if c.HTTP == nil {
		return nil, fmt.Errorf("microsoft graph: the provider has no HTTP client")
	}
	token, err := c.Tokens.Token(ctx, graphResource)
	if err != nil {
		return nil, fmt.Errorf("microsoft graph: token: %w", err)
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, graphBaseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	raw, err := httpx.DecodeBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, graphError(method, path, resp.StatusCode, raw)
	}
	return raw, nil
}

// graphError builds the error for a non-2xx Graph response.
func graphError(method, path string, status int, raw []byte) error {
	code := ""
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil {
		code = env.Error.Code
	}
	excerpt := strings.TrimSpace(string(raw))
	if len(excerpt) > graphBodyMax {
		cut := graphBodyMax
		for cut > 0 && !utf8.RuneStart(excerpt[cut]) {
			cut--
		}
		excerpt = excerpt[:cut]
	}
	if excerpt == "" {
		excerpt = "(no response body)"
	}
	msg := fmt.Sprintf("microsoft graph %s %s: %s", method, path, excerpt)
	if status == http.StatusForbidden {
		msg += " — the app's service principal may need the User Administrator role"
	}
	return &httpx.APIError{Status: status, Code: code, Message: msg, Body: excerpt}
}

// isGraphNotFound reports whether err is a Graph 404.
func isGraphNotFound(err error) bool { return httpx.IsNotFound(err) }

func graphUserPath(id string) string { return "/users/" + url.PathEscape(id) }

// graphPatchUser updates a user's properties.
func graphPatchUser(ctx context.Context, c *clients.Client, id string, body map[string]any) error {
	_, err := graphDo(ctx, c, http.MethodPatch, graphUserPath(id), body)
	return err
}

// graphDeleteUser deletes a user. A missing user is treated as deleted.
func graphDeleteUser(ctx context.Context, c *clients.Client, id string) error {
	_, err := graphDo(ctx, c, http.MethodDelete, graphUserPath(id), nil)
	if err != nil && !isGraphNotFound(err) {
		return err
	}
	return nil
}
