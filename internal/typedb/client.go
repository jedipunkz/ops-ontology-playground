// Package typedb is a minimal client for the TypeDB 3 HTTP API.
package typedb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxBodyBytes = 1 << 20

type TransactionType string

const (
	Read   TransactionType = "read"
	Write  TransactionType = "write"
	Schema TransactionType = "schema"
)

// ErrUnavailable hides transport details (addresses, dial errors) from
// callers that relay errors to an agent. Details go to the server log.
var ErrUnavailable = errors.New("TypeDB is unavailable")

// QueryError is an error TypeDB returned for a query, such as a schema
// constraint violation. Its message is meant to be shown to the agent.
type QueryError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("TypeDB rejected the request (%s): %s", e.Code, e.Message)
}

type Response struct {
	QueryType  string            `json:"queryType"`
	AnswerType string            `json:"answerType"`
	Answers    []json.RawMessage `json:"answers"`
}

type Client struct {
	baseURL    *url.URL
	username   string
	password   string
	httpClient *http.Client

	mu    sync.Mutex
	token string
}

func NewClient(rawURL, username, password string) (*Client, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse TypeDB URL: %w", err)
	}
	if parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("TypeDB URL must be a plain local HTTP URL")
	}
	if !isLoopbackHost(parsed.Hostname()) {
		return nil, errors.New("TypeDB URL must use localhost or a loopback IP")
	}
	if username == "" || password == "" {
		return nil, errors.New("TypeDB username and password are required")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &Client{
		baseURL:  parsed,
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Query runs one query in its own transaction. Write and schema queries are
// committed only when commit is true.
func (c *Client) Query(ctx context.Context, database string, txType TransactionType, query string, commit bool) (Response, error) {
	var response Response
	body, err := c.authorized(ctx, http.MethodPost, "/v1/query", map[string]any{
		"databaseName":    database,
		"transactionType": txType,
		"query":           query,
		"commit":          commit,
	})
	if err != nil {
		return response, err
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return response, fmt.Errorf("decode TypeDB response: %w", err)
	}
	return response, nil
}

// Schema returns the database schema as a TypeQL define query.
func (c *Client) Schema(ctx context.Context, database string) (string, error) {
	body, err := c.authorized(ctx, http.MethodGet, "/v1/databases/"+url.PathEscape(database)+"/schema", nil)
	return string(body), err
}

func (c *Client) DatabaseExists(ctx context.Context, database string) (bool, error) {
	_, err := c.authorized(ctx, http.MethodGet, "/v1/databases/"+url.PathEscape(database), nil)
	var queryErr *QueryError
	if errors.As(err, &queryErr) && queryErr.Status == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) CreateDatabase(ctx context.Context, database string) error {
	_, err := c.authorized(ctx, http.MethodPost, "/v1/databases/"+url.PathEscape(database), nil)
	return err
}

func (c *Client) DeleteDatabase(ctx context.Context, database string) error {
	_, err := c.authorized(ctx, http.MethodDelete, "/v1/databases/"+url.PathEscape(database), nil)
	return err
}

// authorized sends a request with a cached token and signs in again once
// when the token has expired.
func (c *Client) authorized(ctx context.Context, method, path string, payload any) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		token, err := c.currentToken(ctx)
		if err != nil {
			return nil, err
		}
		body, err := c.do(ctx, method, path, token, payload)
		var queryErr *QueryError
		if attempt == 0 && errors.As(err, &queryErr) && queryErr.Status == http.StatusUnauthorized {
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		return body, err
	}
}

func (c *Client) currentToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	body, err := c.do(ctx, http.MethodPost, "/v1/signin", "", map[string]string{
		"username": c.username,
		"password": c.password,
	})
	if err != nil {
		return "", fmt.Errorf("sign in to TypeDB: %w", err)
	}
	var signin struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &signin); err != nil || signin.Token == "" {
		return "", errors.New("TypeDB sign-in returned no token")
	}
	c.token = signin.Token
	return c.token, nil
}

func (c *Client) do(ctx context.Context, method, path, token string, payload any) ([]byte, error) {
	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode TypeDB request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	endpoint := *c.baseURL
	endpoint.Path += path
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("create TypeDB request: %w", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		log.Printf("TypeDB request %s %s failed: %v", method, path, err)
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		log.Printf("read TypeDB response for %s %s: %v", method, path, err)
		return nil, ErrUnavailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		queryErr := &QueryError{Status: response.StatusCode}
		if json.Unmarshal(body, queryErr) != nil || queryErr.Code == "" {
			queryErr.Code = http.StatusText(response.StatusCode)
		}
		return nil, queryErr
	}
	return body, nil
}
