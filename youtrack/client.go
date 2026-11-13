package youtrack

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	apiPrefix      = "/api"
	requestTimeout = 30 * time.Second

	// YouTrack returns at most 42 entries when $top is omitted
	// (devportal: api-concept-pagination); we send it explicitly.
	DefaultPageSize = 42

	maxErrorBodyBytes = 4 << 10

	contentTypeJSON = "application/json"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// NewInsecure skips the verification of the server's certificate, for an
// instance behind a CA this machine does not trust. Whoever sits between the
// two can then read the token.
func NewInsecure(baseURL, token string) *Client {
	c := New(baseURL, token)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	c.http.Transport = transport
	return c
}

// Page with Top == 0 asks for DefaultPageSize entries.
type Page struct {
	Skip int
	Top  int
}

func (p Page) apply(q url.Values) {
	top := p.Top
	if top == 0 {
		top = DefaultPageSize
	}
	q.Set("$top", strconv.Itoa(top))
	q.Set("$skip", strconv.Itoa(p.Skip))
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("youtrack: %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("youtrack: %d %s: %s", e.Status, e.Code, e.Message)
}

func newAPIError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	var body struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	apiErr := &APIError{Status: resp.StatusCode}
	if json.Unmarshal(raw, &body) == nil && (body.Error != "" || body.Description != "") {
		apiErr.Code = body.Error
		apiErr.Message = body.Description
		return apiErr
	}
	apiErr.Message = strings.TrimSpace(string(raw))
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}
	return apiErr
}

func fieldsQuery(fields string) url.Values {
	return url.Values{"fields": {fields}}
}

func pagedQuery(fields string, page Page) url.Values {
	q := fieldsQuery(fields)
	page.apply(q)
	return q
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.send(ctx, http.MethodGet, path, query, nil, "", out)
}

func (c *Client) post(ctx context.Context, path string, query url.Values, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("youtrack: encode request: %w", err)
	}
	return c.send(ctx, http.MethodPost, path, query, bytes.NewReader(raw), contentTypeJSON, out)
}

func (c *Client) delete(ctx context.Context, path string) error {
	return c.send(ctx, http.MethodDelete, path, nil, nil, "", nil)
}

func (c *Client) send(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string, out any) error {
	target := c.baseURL + apiPrefix + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("youtrack: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", contentTypeJSON)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("youtrack: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newAPIError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("youtrack: decode %s %s: %w", method, path, err)
	}
	return nil
}

func segment(id string) string {
	return url.PathEscape(id)
}

func millis(t time.Time) int64 {
	return t.UnixMilli()
}
