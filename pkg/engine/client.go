package engine

import (
	openlane "github.com/theopenlane/go-client"
	"github.com/theopenlane/go-client/graphclient"
)

// Config holds the connection settings for the Openlane API
type Config struct {
	// Host is the base URL of the Openlane API
	Host string
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

// Option configures a Client
type Option func(*Client)

// WithGraphClient supplies the GraphQL client instead of building one from the
// config, so the pull, plan, and apply paths can be driven against a fake
func WithGraphClient(typed graphclient.GraphClient) Option {
	return func(c *Client) {
		c.typed = typed
	}
}

// NewClient builds a Client from the given config
func NewClient(config Config, opts ...Option) (*Client, error) {
	client := &Client{config: config}

	for _, opt := range opts {
		opt(client)
	}

	if client.typed != nil {
		return client, nil
	}

	if config.Token == "" {
		return nil, ErrMissingToken
	}

	if config.Host == "" {
		return nil, ErrMissingHost
	}

	clientOpts := []openlane.ClientOption{
		openlane.WithBaseURL(config.Host),
		openlane.WithCredentials(openlane.Authorization{
			BearerToken: config.Token,
		}),
	}

	if config.OrganizationID != "" {
		clientOpts = append(clientOpts,
			openlane.WithInterceptors(openlane.WithOrganizationHeader(config.OrganizationID)),
		)
	}

	typed, err := openlane.New(clientOpts...)
	if err != nil {
		return nil, err
	}

	client.typed = typed

	return client, nil
}
