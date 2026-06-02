package github

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
	"time"

	"github.com/danielwolfman/prdash/internal/logging"
)

const defaultUserAgent = "prdash"
const maxAttempts = 3

type Client struct {
	token       string
	httpClient  *http.Client
	restBaseURL string
	graphQLURL  string
	userAgent   string
	logger      *logging.Logger
}

type Option func(*Client)

func NewClient(token string, opts ...Option) *Client {
	client := &Client{
		token:       token,
		httpClient:  http.DefaultClient,
		restBaseURL: "https://api.github.com",
		graphQLURL:  "https://api.github.com/graphql",
		userAgent:   defaultUserAgent,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) {
		if httpClient != nil {
			client.httpClient = httpClient
		}
	}
}

func WithBaseURLs(restBaseURL, graphQLURL string) Option {
	return func(client *Client) {
		if strings.TrimSpace(restBaseURL) != "" {
			client.restBaseURL = strings.TrimRight(restBaseURL, "/")
		}
		if strings.TrimSpace(graphQLURL) != "" {
			client.graphQLURL = graphQLURL
		}
	}
}

func WithLogger(logger *logging.Logger) Option {
	return func(client *Client) {
		client.logger = logger
	}
}

func (c *Client) graphql(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(graphqlRequest{Query: query, Variables: variables})
	if err != nil {
		return err
	}

	data, err := c.doWithRetry(ctx, http.MethodPost, c.graphQLURL, body)
	if err != nil {
		return err
	}

	wrapped := graphqlResponse{Data: out}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return err
	}
	if len(wrapped.Errors) > 0 {
		return fmt.Errorf("github graphql error: %s", wrapped.Errors[0].Message)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, values url.Values, out any) error {
	endpoint := c.restBaseURL + path
	if len(values) > 0 {
		endpoint += "?" + values.Encode()
	}

	data, err := c.doWithRetry(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (c *Client) post(ctx context.Context, path string, out any) error {
	data, err := c.doWithRetry(ctx, http.MethodPost, c.restBaseURL+path, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (c *Client) doWithRetry(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		reqBody := io.Reader(nil)
		if body != nil {
			reqBody = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)

		start := time.Now()
		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.logRequest(method, endpoint, attempt, 0, time.Since(start), err)
			lastErr = err
			if attempt == maxAttempts {
				return nil, err
			}
			if err := sleepRetry(ctx, retryDelay(attempt, nil)); err != nil {
				return nil, err
			}
			continue
		}

		data, err := readResponse(resp)
		c.logRequest(method, endpoint, attempt, resp.StatusCode, time.Since(start), err)
		if err == nil {
			return data, nil
		}
		lastErr = err

		var apiErr apiError
		if !errors.As(err, &apiErr) || !apiErr.Transient() || attempt == maxAttempts {
			return nil, err
		}
		if err := sleepRetry(ctx, retryDelay(attempt, resp)); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) logRequest(method, endpoint string, attempt, status int, duration time.Duration, err error) {
	if c.logger == nil {
		return
	}
	fields := map[string]any{
		"method":      method,
		"api_url":     endpoint,
		"attempt":     attempt,
		"status":      status,
		"duration_ms": duration.Milliseconds(),
	}
	if err != nil {
		fields["error"] = err.Error()
		c.logger.Warn("github_request", fields)
		return
	}
	c.logger.Info("github_request", fields)
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func readResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return data, nil
	}
	return nil, apiError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Body:       strings.TrimSpace(string(data)),
	}
}

type apiError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e apiError) Error() string {
	if e.Body == "" {
		return "github api " + e.Status
	}
	return fmt.Sprintf("github api %s: %s", e.Status, e.Body)
}

func (e apiError) Transient() bool {
	return e.StatusCode == http.StatusTooManyRequests ||
		e.StatusCode == http.StatusBadGateway ||
		e.StatusCode == http.StatusServiceUnavailable ||
		e.StatusCode == http.StatusGatewayTimeout
}

func retryDelay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After")); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	return time.Duration(attempt*250) * time.Millisecond
}

func sleepRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphqlResponse struct {
	Data   any `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}
