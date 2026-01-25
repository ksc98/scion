// Package hubclient provides a Go client for the Scion Hub API.
package hubclient

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ptone/scion-agent/pkg/apiclient"
)

// Client is the interface for the Hub API client.
// This interface enables mocking for tests.
type Client interface {
	// Agents returns the agent operations interface.
	Agents() AgentService

	// Groves returns the grove operations interface.
	Groves() GroveService

	// RuntimeHosts returns the runtime host operations interface.
	RuntimeHosts() RuntimeHostService

	// Templates returns the template operations interface.
	Templates() TemplateService

	// Users returns the user operations interface.
	Users() UserService

	// Auth returns the authentication operations interface.
	Auth() AuthService

	// Health checks API availability.
	Health(ctx context.Context) (*HealthResponse, error)
}

// client is the concrete implementation of Client.
type client struct {
	transport *apiclient.Transport

	agents       *agentService
	groves       *groveService
	runtimeHosts *runtimeHostService
	templates    *templateService
	users        *userService
	authService  *authService
}

// New creates a new Hub API client.
func New(baseURL string, opts ...Option) (Client, error) {
	c := &client{
		transport: apiclient.NewTransport(baseURL),
	}

	for _, opt := range opts {
		opt(c)
	}

	// Initialize service implementations
	c.agents = &agentService{c: c}
	c.groves = &groveService{c: c}
	c.runtimeHosts = &runtimeHostService{c: c}
	c.templates = &templateService{c: c}
	c.users = &userService{c: c}
	c.authService = &authService{c: c}

	return c, nil
}

// Agents returns the agent operations interface.
func (c *client) Agents() AgentService {
	return c.agents
}

// Groves returns the grove operations interface.
func (c *client) Groves() GroveService {
	return c.groves
}

// RuntimeHosts returns the runtime host operations interface.
func (c *client) RuntimeHosts() RuntimeHostService {
	return c.runtimeHosts
}

// Templates returns the template operations interface.
func (c *client) Templates() TemplateService {
	return c.templates
}

// Users returns the user operations interface.
func (c *client) Users() UserService {
	return c.users
}

// Auth returns the authentication operations interface.
func (c *client) Auth() AuthService {
	return c.authService
}

// Health checks API availability.
func (c *client) Health(ctx context.Context) (*HealthResponse, error) {
	resp, err := c.transport.Get(ctx, "/healthz", nil)
	if err != nil {
		return nil, err
	}
	return apiclient.DecodeResponse[HealthResponse](resp)
}

// HealthResponse is the response from health check.
type HealthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Uptime  string            `json:"uptime"`
	Checks  map[string]string `json:"checks,omitempty"`
}

// Option configures a Hub client.
type Option func(*client)

// WithBearerToken sets Bearer token authentication.
func WithBearerToken(token string) Option {
	return func(c *client) {
		c.transport.Auth = &apiclient.BearerAuth{Token: token}
	}
}

// WithAPIKey sets API key authentication.
func WithAPIKey(key string) Option {
	return func(c *client) {
		c.transport.Auth = &apiclient.APIKeyAuth{Key: key}
	}
}

// WithAuthenticator sets a custom authenticator.
func WithAuthenticator(auth apiclient.Authenticator) Option {
	return func(c *client) {
		c.transport.Auth = auth
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *client) {
		c.transport.HTTPClient = hc
	}
}

// WithTimeout sets the request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *client) {
		c.transport.HTTPClient.Timeout = d
	}
}

// WithRetry configures retry behavior.
func WithRetry(maxRetries int, wait time.Duration) Option {
	return func(c *client) {
		c.transport.MaxRetries = maxRetries
		c.transport.RetryWait = wait
	}
}

// WithDevToken sets a development token for authentication.
// This is equivalent to WithBearerToken but makes the intent clearer.
func WithDevToken(token string) Option {
	return func(c *client) {
		c.transport.Auth = &apiclient.BearerAuth{Token: token}
	}
}

// WithAutoDevAuth attempts to load a development token automatically.
// It checks in order:
// 1. SCION_DEV_TOKEN environment variable
// 2. SCION_DEV_TOKEN_FILE environment variable (path to token file)
// 3. Default token file (~/.scion/dev-token)
// If no token is found, authentication is not configured.
func WithAutoDevAuth() Option {
	return func(c *client) {
		token, source := apiclient.ResolveDevTokenWithSource()
		if token != "" {
			c.transport.Auth = &apiclient.BearerAuth{Token: token}
			if os.Getenv("SCION_DEBUG") != "" {
				// Truncate token for display
				displayToken := token
				if len(displayToken) > 20 {
					displayToken = displayToken[:20] + "..."
				}
				fmt.Fprintf(os.Stderr, "[DEBUG] Dev auth token: %s (source: %s)\n", displayToken, source)
			}
		} else if os.Getenv("SCION_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[DEBUG] No dev auth token found\n")
		}
	}
}
