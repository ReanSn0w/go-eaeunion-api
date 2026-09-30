package eaeunion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// StaticToken supplies an already obtained access token.
type StaticToken string

func (t StaticToken) Token(context.Context) (string, error) {
	if strings.TrimSpace(string(t)) == "" {
		return "", errors.New("empty access token")
	}
	return string(t), nil
}

type credentialProvider struct {
	credentials ClientCredentials
	client      *http.Client
	timeout     time.Duration
	maxBytes    int64
	mu          sync.Mutex
	token       string
	expires     time.Time
}

func newCredentialProvider(credentials ClientCredentials, client *http.Client, timeout time.Duration, maxBytes int64) (TokenProvider, error) {
	if _, err := parseEndpoint(credentials.TokenURL); err != nil {
		return nil, fmt.Errorf("token URL: %w", err)
	}
	if credentials.ClientID == "" || credentials.ClientSecret == "" {
		return nil, errors.New("client ID and secret are required")
	}
	return &credentialProvider{credentials: credentials, client: client, timeout: timeout, maxBytes: maxBytes}, nil
}

func (p *credentialProvider) Token(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", errors.New("context is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && time.Now().Before(p.expires) {
		return p.token, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {p.credentials.ClientID}, "client_secret": {p.credentials.ClientSecret}}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, p.credentials.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := readResponse(p.client, req, p.maxBytes, p.credentials.ClientSecret, p.credentials.ClientID)
	if err != nil {
		return "", fmt.Errorf("obtain OAuth token: %w", err)
	}
	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode OAuth token: %w", err)
	}
	if result.AccessToken == "" || (result.TokenType != "" && !strings.EqualFold(result.TokenType, "Bearer")) || result.ExpiresIn <= 0 {
		return "", errors.New("invalid OAuth token response")
	}
	margin := 30 * time.Second
	lifetime := time.Duration(result.ExpiresIn) * time.Second
	if lifetime <= margin {
		margin = lifetime / 10
	}
	p.token = result.AccessToken
	p.expires = time.Now().Add(lifetime - margin)
	return p.token, nil
}
