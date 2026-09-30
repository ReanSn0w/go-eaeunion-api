package eaeunion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var ErrResponseTooLarge = errors.New("API response exceeds configured size limit")

// HTTPError reports a failed outer HTTP request. Message is bounded and may be
// empty; callers should branch on StatusCode rather than message text.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("API returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("API returned HTTP %d: %s", e.StatusCode, e.Message)
}

// Find reads one page from a collection. Pagination is explicit; use Walk to
// traverse multiple pages. The collection is selected by its published name.
func (c *Client) Find(ctx context.Context, collection string, query Query) (Page, error) {
	if c == nil {
		return Page{}, errors.New("nil client")
	}
	if strings.TrimSpace(collection) == "" {
		return Page{}, errors.New("collection is required")
	}
	if ctx == nil {
		return Page{}, errors.New("context is required")
	}
	body, params, err := encodeQuery(query)
	if err != nil {
		return Page{}, err
	}
	params.Set("collection", collection)
	endpoint := *c.endpoint
	endpoint.RawQuery = params.Encode()
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return Page{}, fmt.Errorf("build API request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if c.mode != Anonymous {
		return Page{}, errors.New("configured authorization mode is unavailable")
	}
	return c.send(req)
}

func (c *Client) send(req *http.Request) (Page, error) {
	response, err := c.httpClient.Do(req)
	if err != nil {
		return Page{}, fmt.Errorf("send API request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return Page{}, fmt.Errorf("read API response: %w", err)
	}
	if int64(len(body)) > c.maxResponseBytes {
		return Page{}, ErrResponseTooLarge
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 512 {
			message = message[:512]
		}
		return Page{}, &HTTPError{StatusCode: response.StatusCode, Message: message}
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Page{}, fmt.Errorf("decode API response: %w", err)
	}
	if envelope == nil || envelope["result"] == nil || envelope["responseSize"] == nil {
		return Page{}, errors.New("API response is missing result or responseSize")
	}
	var page Page
	if err := json.Unmarshal(body, &page); err != nil {
		return Page{}, fmt.Errorf("decode API page: %w", err)
	}
	if page.Result == nil || page.ResponseSize != int64(len(page.Result)) {
		return Page{}, errors.New("API response has inconsistent result size")
	}
	return page, nil
}

func encodeQuery(query Query) ([]byte, url.Values, error) {
	filter := []byte("{}")
	if query.Filter != nil {
		var err error
		filter, err = json.Marshal(query.Filter)
		if err != nil {
			return nil, nil, fmt.Errorf("encode filter: %w", err)
		}
		filter = bytes.TrimSpace(filter)
		if len(filter) == 0 || filter[0] != '{' || !json.Valid(filter) {
			return nil, nil, errors.New("filter must be a JSON object")
		}
	}
	params := url.Values{}
	if query.Limit != nil {
		if *query.Limit <= 0 {
			return nil, nil, errors.New("limit must be positive; zero requests the entire collection")
		}
		params.Set("limit", strconv.Itoa(*query.Limit))
	}
	if query.Skip != nil {
		if *query.Skip < 0 {
			return nil, nil, errors.New("skip cannot be negative")
		}
		params.Set("skip", strconv.Itoa(*query.Skip))
	}
	if query.SkipCount != nil {
		params.Set("skipCount", strconv.FormatBool(*query.SkipCount))
	}
	if len(query.Sort) > 0 {
		encoded, err := encodeOrdered(query.Sort, func(field SortField) (string, int) { return field.Name, field.Direction }, -1, 1)
		if err != nil {
			return nil, nil, fmt.Errorf("sort: %w", err)
		}
		params.Set("sort", encoded)
	}
	if len(query.Fields) > 0 {
		encoded, err := encodeOrdered(query.Fields, func(field Field) (string, int) { return field.Name, field.Include }, 0, 1)
		if err != nil {
			return nil, nil, fmt.Errorf("fields: %w", err)
		}
		params.Set("fields", encoded)
	}
	return filter, params, nil
}

func encodeOrdered[T any](fields []T, extract func(T) (string, int), first, second int) (string, error) {
	var output strings.Builder
	output.WriteByte('{')
	seen := make(map[string]struct{}, len(fields))
	for i, field := range fields {
		name, value := extract(field)
		if strings.TrimSpace(name) == "" || (value != first && value != second) {
			return "", errors.New("field name or value is invalid")
		}
		if _, exists := seen[name]; exists {
			return "", fmt.Errorf("duplicate field %q", name)
		}
		seen[name] = struct{}{}
		if i > 0 {
			output.WriteByte(',')
		}
		encodedName, _ := json.Marshal(name)
		output.Write(encodedName)
		output.WriteByte(':')
		output.WriteString(strconv.Itoa(value))
	}
	output.WriteByte('}')
	return output.String(), nil
}
