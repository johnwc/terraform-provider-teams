package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingTransport records the requests it receives and answers 200.
type recordingTransport struct{ reqs []*http.Request }

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.reqs = append(t.reqs, req)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}, Request: req}, nil
}

func TestTNMHeaderTransport(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://api.example/Skype.TelephoneNumberMgmt/cmdlet/assignment?identity=x&phoneNumber=%2B18005550100", tnmApplicationID},
		{"https://api.example/Skype.TelephoneNumberMgmt/cmdlet/assignments", tnmApplicationID},
		{"https://api.example/Teams.VoiceApps/callqueues", ""},
		{"https://api.example/Skype.TelephoneNumberMgmtX/cmdlet", ""},
		{"https://api.example/teams/Skype.TelephoneNumberMgmt/cmdlet", ""},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			rec := &recordingTransport{}
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			resp, err := tnmHeaderTransport{next: rec}.RoundTrip(req)
			if err != nil {
				t.Fatalf("RoundTrip: %v", err)
			}
			if err := resp.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if len(rec.reqs) != 1 {
				t.Fatalf("next saw %d requests; want 1", len(rec.reqs))
			}
			if got := rec.reqs[0].Header.Get(tnmApplicationIDKey); got != tc.want {
				t.Errorf("header = %q; want %q", got, tc.want)
			}
			if req.Header.Get(tnmApplicationIDKey) != "" {
				t.Error("the caller's request was modified")
			}
		})
	}
}

func TestTNMHeaderTransportServer(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(tnmApplicationIDKey)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client := &http.Client{Transport: tnmHeaderTransport{}}
	for path, want := range map[string]string{
		"/Skype.TelephoneNumberMgmt/cmdlet/assignments?telephoneNumber=%2B18005550100": tnmApplicationID,
		"/Teams.PlatformService/v2/ApplicationInstances":                               "",
	} {
		got = "unset"
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: header = %q; want %q", path, got, want)
		}
	}
}
