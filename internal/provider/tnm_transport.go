package provider

import (
	"net/http"
	"strings"
)

const (
	tnmPathPrefix       = "/Skype.TelephoneNumberMgmt/"
	tnmApplicationIDKey = "X-MS-TNM-ApplicationId"
	// tnmApplicationID is the value the MicrosoftTeams module sends.
	tnmApplicationID = "45992572-c0f0-47eb-8ee8-f599d28cb950"
)

// tnmHeaderTransport sets the X-MS-TNM-ApplicationId header on telephone number
// management requests and passes every other request through unchanged.
type tnmHeaderTransport struct{ next http.RoundTripper }

func (t tnmHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	if !strings.HasPrefix(req.URL.Path, tnmPathPrefix) {
		return next.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	r.Header.Set(tnmApplicationIDKey, tnmApplicationID)
	return next.RoundTrip(r)
}
