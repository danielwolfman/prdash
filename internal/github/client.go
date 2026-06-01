package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultUserAgent = "prdash"

type Client struct {
	token       string
	httpClient  *http.Client
	restBaseURL string
	graphQLURL  string
	userAgent   string
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

func (c *Client) graphql(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(graphqlRequest{Query: query, Variables: variables})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphQLURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := readResponse(resp)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := readResponse(resp)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
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
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return data, nil
	}
	return nil, fmt.Errorf("github api %s: %s", resp.Status, strings.TrimSpace(string(data)))
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
