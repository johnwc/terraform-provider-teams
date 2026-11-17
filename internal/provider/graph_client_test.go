package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/terraprovider/go-msadmin/auth"
	"github.com/terraprovider/go-msadmin/httpx"

	"github.com/terraprovider/terraform-provider-teams/internal/clients"
)

// graphFake answers every request with one canned response and records the request.
type graphFake struct {
	status int
	body   string
	req    *http.Request
	sent   string
}

func (f *graphFake) RoundTrip(req *http.Request) (*http.Response, error) {
	f.req = req
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		f.sent = string(b)
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(f.body)), Header: http.Header{}, Request: req}, nil
}

// tokenRecorder records the resource it was asked for.
type tokenRecorder struct{ resource string }

func (t *tokenRecorder) Token(_ context.Context, resource string) (string, error) {
	t.resource = resource
	return "tok", nil
}

func testGraphClient(f *graphFake, tp auth.TokenProvider) *clients.Client {
	return &clients.Client{Tokens: tp, HTTP: &http.Client{Transport: f}}
}

func TestGraphPatchUser(t *testing.T) {
	f := &graphFake{status: http.StatusNoContent}
	tp := &tokenRecorder{}
	if err := graphPatchUser(context.Background(), testGraphClient(f, tp), "obj-1", map[string]any{"displayName": "New"}); err != nil {
		t.Fatalf("graphPatchUser: %v", err)
	}
	if f.req.Method != http.MethodPatch || f.req.URL.String() != "https://graph.microsoft.com/v1.0/users/obj-1" {
		t.Errorf("request = %s %s", f.req.Method, f.req.URL)
	}
	if got := f.req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q", got)
	}
	if tp.resource != "https://graph.microsoft.com" {
		t.Errorf("token resource = %q", tp.resource)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(f.sent), &sent); err != nil || sent["displayName"] != "New" {
		t.Errorf("body = %q", f.sent)
	}
}

func TestGraphDeleteUser(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"deleted", http.StatusNoContent, false},
		{"already gone", http.StatusNotFound, false},
		{"forbidden", http.StatusForbidden, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &graphFake{status: tc.status, body: `{"error":{"code":"x","message":"y"}}`}
			err := graphDeleteUser(context.Background(), testGraphClient(f, auth.StaticToken("tok")), "obj-1")
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v; wantErr %v", err, tc.wantErr)
			}
			if f.req.Method != http.MethodDelete {
				t.Errorf("method = %s", f.req.Method)
			}
		})
	}
}

func TestGraphError(t *testing.T) {
	f := &graphFake{status: http.StatusNotFound, body: `{"error":{"code":"Request_ResourceNotFound","message":"Resource 'obj-1' does not exist."}}`}
	_, err := graphDo(context.Background(), testGraphClient(f, auth.StaticToken("tok")), http.MethodGet, graphUserPath("obj-1"), nil)
	if !isGraphNotFound(err) {
		t.Fatalf("isGraphNotFound(%v) = false", err)
	}
	var ae *httpx.APIError
	if !errors.As(err, &ae) || ae.Code != "Request_ResourceNotFound" {
		t.Errorf("err = %#v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "404") || !strings.Contains(msg, "does not exist") {
		t.Errorf("message = %q; want status and body excerpt", msg)
	}

	long := graphError(http.MethodGet, "/users/x", http.StatusInternalServerError, []byte(strings.Repeat("a", 2000)))
	if isGraphNotFound(long) {
		t.Error("500 reported as not found")
	}
	if !errors.As(long, &ae) || len(ae.Body) != graphBodyMax {
		t.Errorf("excerpt = %#v; want %d bytes", long, graphBodyMax)
	}
	if isGraphNotFound(errors.New("plain")) || isGraphNotFound(nil) {
		t.Error("non-API errors reported as not found")
	}
}

func TestGraphErrorTruncatesOnRuneBoundary(t *testing.T) {
	body := "a" + strings.Repeat("é", graphBodyMax)
	err := graphError(http.MethodGet, "/users/x", http.StatusInternalServerError, []byte(body))
	var ae *httpx.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %#v", err)
	}
	if !utf8.ValidString(ae.Body) || len(ae.Body) > graphBodyMax {
		t.Errorf("excerpt has %d bytes, valid UTF-8 = %v", len(ae.Body), utf8.ValidString(ae.Body))
	}
	if !utf8.ValidString(err.Error()) {
		t.Error("error message is not valid UTF-8")
	}
}

func TestGraphNoTokens(t *testing.T) {
	if _, err := graphDo(context.Background(), &clients.Client{}, http.MethodGet, "/users/x", nil); err == nil {
		t.Error("expected an error without a token source")
	}
}

func TestGraphNoHTTPClient(t *testing.T) {
	c := &clients.Client{Tokens: auth.StaticToken("tok")}
	if _, err := graphDo(context.Background(), c, http.MethodGet, "/users/x", nil); err == nil {
		t.Error("expected an error without an HTTP client")
	}
}
