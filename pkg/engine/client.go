package engine

import (
	openlane "github.com/theopenlane/go-client"
	"github.com/theopenlane/go-client/graphclient"
)

// Config holds the connection settings for the Openlane API
type Config struct {
	// Host is the base URL of the Openlane API
	Host string `koanf:"host" default:"https://api.theopenlane.io"`
	// Token is the API token used for authentication
	Token string
	// OrganizationID optionally scopes requests to a specific organization
	OrganizationID string
}

// Client wraps the generated Openlane GraphQL client
type Client struct {
	config Config
	typed  graphclient.GraphClient
}

// NewClient builds a Client from the given config
func NewClient(config Config) (*Client, error) {
	if config.Token == "" {
		return nil, ErrMissingToken
	}

	opts := []openlane.ClientOption{
		openlane.WithBaseURL(config.Host),
		openlane.WithCredentials(openlane.Authorization{
			BearerToken: config.Token,
		}),
	}

	if config.OrganizationID != "" {
		opts = append(opts,
			openlane.WithInterceptors(openlane.WithOrganizationHeader(config.OrganizationID)),
		)
	}

	typed, err := openlane.New(opts...)
	if err != nil {
		return nil, err
	}

	return &Client{config: config, typed: typed}, nil
}
