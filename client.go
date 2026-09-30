// Package eaeunion provides a collection-agnostic client for the EAEU REST API.
package eaeunion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout     = 30 * time.Second
	defaultMaxResponse = 16 << 20
	defaultPageLimit   = 500
)

// AccessMode selects how the API request is sent.
type AccessMode uint8

const (
	// Anonymous sends requests directly to the configured endpoint.
	Anonymous AccessMode = iota
	// DirectToken adds a bearer token to a direct request.
	DirectToken
	// Gateway sends the request through the EAEU policy gateway.
	Gateway
)

// TokenProvider supplies a bearer token. Implementations must be safe to call
// concurrently when a Client is used from multiple goroutines.
type TokenProvider interface {
	Token(context.Context) (string, error)
}

// ClientCredentials are used with the OAuth client_credentials grant.
type ClientCredentials struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
}

// GatewayConfig identifies the outer request gateway and its target service.
type GatewayConfig struct {
	URL        string
	ServiceKey string
	CountryTo  string
}

// Config contains immutable settings shared by requests to one REST endpoint.
type Config struct {
	Endpoint         string
	HTTPClient       *http.Client
	Timeout          time.Duration
	MaxResponseBytes int64
	Mode             AccessMode
	TokenProvider    TokenProvider
	Credentials      *ClientCredentials
	Gateway          *GatewayConfig
}

// SortField is one sort criterion. Criteria are serialized in slice order.
type SortField struct {
	Name      string
	Direction int // 1 ascending, -1 descending
}

// Field selects (1) or excludes (0) one result field.
type Field struct {
	Name    string
	Include int
}

// Query defines one collection query. Nil optional values are omitted.
// Filter accepts a JSON-marshalable value or a valid json.RawMessage.
type Query struct {
	Filter    any
	Limit     *int
	Skip      *int
	Sort      []SortField
	Fields    []Field
	SkipCount *bool
}

// Page is the documented response envelope. Counts are nil with skipCount=true.
type Page struct {
	Result           []json.RawMessage `json:"result"`
	TotalDocuments   *int64            `json:"totalDocuments,omitempty"`
	MatchedDocuments *int64            `json:"matchedDocuments,omitempty"`
	ResponseSize     int64             `json:"responseSize"`
}

// DecodePage decodes documents into the caller's type without rounding numbers
// stored in interface{} fields.
func DecodePage[T any](page Page) ([]T, error) {
	items := make([]T, 0, len(page.Result))
	for i, raw := range page.Result {
		var item T
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("decode document %d: %w", i, err)
		}
		items = append(items, item)
	}
	return items, nil
}

// Client can be shared by goroutines. The supplied HTTPClient and token
// provider must also be safe for concurrent use.
type Client struct {
	endpoint         *url.URL
	httpClient       *http.Client
	timeout          time.Duration
	maxResponseBytes int64
	mode             AccessMode
	tokenProvider    TokenProvider
	credentials      *ClientCredentials
	gateway          *GatewayConfig
}

// NewClient validates its endpoint and applies conservative defaults.
func NewClient(cfg Config) (*Client, error) {
	endpoint, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if cfg.Timeout < 0 || cfg.MaxResponseBytes < 0 {
		return nil, errors.New("timeout and maximum response size cannot be negative")
	}
	if cfg.Mode > Gateway {
		return nil, errors.New("unknown access mode")
	}
	if cfg.TokenProvider != nil && cfg.Credentials != nil {
		return nil, errors.New("provide either a token provider or client credentials")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	maxResponse := cfg.MaxResponseBytes
	if maxResponse == 0 {
		maxResponse = defaultMaxResponse
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client := &Client{
		endpoint: endpoint, httpClient: httpClient, timeout: timeout,
		maxResponseBytes: maxResponse, mode: cfg.Mode,
		tokenProvider: cfg.TokenProvider,
	}
	if cfg.Credentials != nil {
		credentials := *cfg.Credentials
		client.credentials = &credentials
	}
	if cfg.Gateway != nil {
		gateway := *cfg.Gateway
		client.gateway = &gateway
	}
	return client, nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("endpoint must be an absolute HTTP URL without credentials, query, or fragment")
	}
	return u, nil
}
