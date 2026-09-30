package eaeunion

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type gatewayHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type gatewayRequest struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	ServiceKey  string          `json:"serviceKey"`
	CountryTo   string          `json:"countryTo"`
	Path        string          `json:"path"`
	RequestType string          `json:"requestType"`
	Headers     []gatewayHeader `json:"headers"`
	Body        string          `json:"body,omitempty"`
}
type gatewayResponse struct {
	Status  string `json:"status"`
	Request struct {
		Status         string `json:"status"`
		ResponseStatus int    `json:"responseStatus"`
		ResponseBody   string `json:"responseBody"`
	} `json:"request"`
}

// GatewayError reports a failed inner request without echoing the gateway's
// response body, which can contain bearer credentials.
type GatewayError struct {
	Status         string
	RequestStatus  string
	ResponseStatus int
}

func (e *GatewayError) Error() string {
	return fmt.Sprintf("gateway request failed: status=%s requestStatus=%s responseStatus=%d", e.Status, e.RequestStatus, e.ResponseStatus)
}

func (c *Client) findViaGateway(ctx context.Context, endpoint url.URL, filter []byte) (Page, error) {
	access, err := c.tokenProvider.Token(ctx)
	if err != nil {
		return Page{}, fmt.Errorf("obtain access token: %w", err)
	}
	if access == "" {
		return Page{}, errors.New("token provider returned an empty token")
	}
	pathPrefix := "/" + strings.Trim(c.gateway.ServiceKey, "/")
	if !strings.HasPrefix(endpoint.Path, pathPrefix+"/") {
		return Page{}, errors.New("endpoint path does not match gateway service key")
	}
	path := strings.TrimPrefix(endpoint.Path, pathPrefix)
	if endpoint.RawQuery != "" {
		path += "?" + endpoint.RawQuery
	}
	for attempt := 0; attempt < 2; attempt++ {
		group, err := c.getGroupToken(ctx, access, attempt > 0)
		if err != nil {
			return Page{}, err
		}
		response, err := c.gatewayCall(ctx, gatewayRequest{Type: "HTTP", ServiceKey: c.gateway.ServiceKey, CountryTo: c.gateway.CountryTo, Path: path, RequestType: "POST", Headers: []gatewayHeader{{"Authorization", "Bearer " + group}, {"Content-Type", "text/plain"}}, Body: string(filter)}, access, group)
		if err != nil {
			return Page{}, err
		}
		if response.Status == "OK" && response.Request.Status == "RESPONSE_SUCCESS" && response.Request.ResponseStatus >= 200 && response.Request.ResponseStatus < 300 {
			return decodePage([]byte(response.Request.ResponseBody))
		}
		if attempt == 0 && response.Request.ResponseStatus == http.StatusUnauthorized {
			continue
		}
		return Page{}, &GatewayError{response.Status, response.Request.Status, response.Request.ResponseStatus}
	}
	return Page{}, errors.New("gateway authentication failed")
}

func (c *Client) getGroupToken(ctx context.Context, access string, force bool) (string, error) {
	c.groupMu.Lock()
	defer c.groupMu.Unlock()
	if !force && c.groupToken != "" && c.groupAccessToken == access {
		return c.groupToken, nil
	}
	response, err := c.gatewayCall(ctx, gatewayRequest{Type: "HTTP", ServiceKey: "platform_svc", CountryTo: "EEC", Path: "/token/login_by_token", RequestType: "GET", Headers: []gatewayHeader{{"Authorization", "Bearer " + access}}}, access)
	if err != nil {
		return "", err
	}
	if response.Status != "OK" || response.Request.Status != "RESPONSE_SUCCESS" || response.Request.ResponseStatus != 200 || response.Request.ResponseBody == "" {
		return "", &GatewayError{response.Status, response.Request.Status, response.Request.ResponseStatus}
	}
	c.groupToken = response.Request.ResponseBody
	c.groupAccessToken = access
	return c.groupToken, nil
}

func (c *Client) gatewayCall(ctx context.Context, payload gatewayRequest, secrets ...string) (gatewayResponse, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return gatewayResponse{}, fmt.Errorf("generate request ID: %w", err)
	}
	payload.ID = hex.EncodeToString(id)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("encode gateway request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gateway.URL, bytes.NewReader(encoded))
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("build gateway request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if payload.ServiceKey == "platform_svc" {
		req.Header.Set("grant_type", "refresh_token")
	}
	body, err := readResponse(c.httpClient, req, c.maxResponseBytes, secrets...)
	if err != nil {
		return gatewayResponse{}, err
	}
	var result gatewayResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return gatewayResponse{}, fmt.Errorf("decode gateway response: %w", err)
	}
	return result, nil
}
