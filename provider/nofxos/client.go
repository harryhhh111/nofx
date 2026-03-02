// Package nofxos provides data access to the NofxOS API (https://nofxos.ai)
// for quantitative trading data including AI500 scores, OI rankings,
// fund flow (NetFlow), price rankings, and coin details.
package nofxos

import (
	"io/ioutil"
	"net/http"
	"nofx/security"
	"strings"
	"sync"
	"time"
)

// Default configuration
const (
	DefaultBaseURL = "https://nofxos.ai"
	DefaultTimeout = 30 * time.Second
	DefaultAuthKey = "cm_568c67eae410d912c54c"
)

// Client is the NofxOS API client
type Client struct {
	BaseURL     string
	AuthKey     string
	Timeout     time.Duration
	customURL   string // Full custom URL for AI500 (optional)
	isCustomURL bool   // Whether using custom URL mode
	mu          sync.RWMutex
}

var (
	defaultClient *Client
	clientOnce    sync.Once
)

// DefaultClient returns the singleton default client
func DefaultClient() *Client {
	clientOnce.Do(func() {
		defaultClient = &Client{
			BaseURL: DefaultBaseURL,
			AuthKey: DefaultAuthKey,
			Timeout: DefaultTimeout,
		}
	})
	return defaultClient
}

// NewClient creates a new NofxOS API client
func NewClient(baseURL, authKey string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if authKey == "" {
		authKey = DefaultAuthKey
	}
	return &Client{
		BaseURL: baseURL,
		AuthKey: authKey,
		Timeout: DefaultTimeout,
	}
}

// SetConfig updates client configuration
func (c *Client) SetConfig(baseURL, authKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if baseURL != "" {
		c.BaseURL = baseURL
	}
	if authKey != "" {
		c.AuthKey = authKey
	}
}

// GetBaseURL returns the current base URL
func (c *Client) GetBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BaseURL
}

// GetAuthKey returns the current auth key
func (c *Client) GetAuthKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AuthKey
}

// IsCustomURL returns whether the client is using a custom URL
func (c *Client) IsCustomURL() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isCustomURL
}

// GetCustomURL returns the custom URL if set
func (c *Client) GetCustomURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.customURL
}

// doCustomURLRequest performs an HTTP GET request to the custom URL
// Note: Uses regular HTTP client with proxy support but without SSRF protection
// since the URL is explicitly configured by the user
func (c *Client) doCustomURLRequest() ([]byte, error) {
	c.mu.RLock()
	customURL := c.customURL
	timeout := c.Timeout
	c.mu.RUnlock()

	// Create HTTP client with proxy support (from environment) but no SSRF protection
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment, // Support HTTP_PROXY/HTTPS_PROXY env vars
		},
	}

	resp, err := client.Get(customURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return body, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return body, nil
}

// doRequest performs an HTTP GET request with authentication
func (c *Client) doRequest(endpoint string) ([]byte, error) {
	c.mu.RLock()
	baseURL := c.BaseURL
	authKey := c.AuthKey
	timeout := c.Timeout
	c.mu.RUnlock()

	url := baseURL + endpoint
	if !strings.Contains(url, "auth=") {
		if strings.Contains(url, "?") {
			url += "&auth=" + authKey
		} else {
			url += "?auth=" + authKey
		}
	}

	resp, err := security.SafeGet(url, timeout)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return body, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return body, nil
}

// APIError represents an API error response
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return e.Message
}

// ExtractAuthKey extracts auth key from a URL string
func ExtractAuthKey(url string) string {
	if idx := strings.Index(url, "auth="); idx != -1 {
		authKey := url[idx+5:]
		if ampIdx := strings.Index(authKey, "&"); ampIdx != -1 {
			authKey = authKey[:ampIdx]
		}
		return authKey
	}
	return ""
}

// NewClientWithCustomURL creates a client for custom API URLs
// The custom URL is used directly without appending the standard endpoint path
func NewClientWithCustomURL(fullURL string) *Client {
	// Parse the URL to extract base URL for logging
	baseURL := fullURL
	if idx := strings.Index(fullURL, "/api/"); idx != -1 {
		baseURL = fullURL[:idx]
	}
	return &Client{
		BaseURL:     baseURL,
		AuthKey:     "", // Custom URLs may not need auth
		Timeout:     DefaultTimeout,
		customURL:   fullURL,
		isCustomURL: true,
	}
}
