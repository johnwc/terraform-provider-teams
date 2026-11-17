// Package clients holds the configured Teams admin API client shared by all
// resources.
package clients

import (
	"net/http"

	"github.com/terraprovider/go-msadmin/auth"
	"github.com/terraprovider/go-teams/cs"
	"github.com/terraprovider/go-teams/teamsapi"
)

// Client is passed to every resource/data source via Configure.
type Client struct {
	API      *teamsapi.Client
	CS       *cs.Service
	TenantID string
	Tokens   auth.TokenProvider // token source for Microsoft Graph calls
	HTTP     *http.Client       // HTTP client for Microsoft Graph calls
}
