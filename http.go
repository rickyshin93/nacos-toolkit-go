package nacostoolkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Connection describes an HTTP Nacos server and its optional credentials.
type Connection struct {
	ServerAddr string
	Namespace  string
	Username   string
	Password   string
}

// ConfigRef identifies a Nacos configuration.
type ConfigRef struct {
	DataID string
	Group  string
}

// HTTPError reports a non-successful response from Nacos.
type HTTPError struct {
	Operation  string
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s failed: HTTP %d", e.Operation, e.StatusCode)
}

// HTTPSource reads Nacos configurations over OpenAPI v1.
type HTTPSource struct {
	base   url.URL
	conn   Connection
	client *http.Client

	login chan struct{}
	token *string
}

// NewHTTPSource creates a source without making a network request. ServerAddr
// accepts host:port or an HTTP(S) origin.
func NewHTTPSource(conn Connection) (*HTTPSource, error) {
	addr := conn.ServerAddr
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	base, err := url.Parse(addr)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") ||
		base.Hostname() == "" || (base.Path != "" && base.Path != "/") ||
		base.User != nil || base.ForceQuery || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("invalid Nacos server address: expected host:port or an HTTP(S) origin")
	}
	return &HTTPSource{
		base:   *base,
		conn:   conn,
		client: &http.Client{Timeout: 10 * time.Second},
		login:  make(chan struct{}, 1),
	}, nil
}

// GetConfig reads the raw configuration. It logs in lazily when either
// credential is present, then reuses the access token for this source.
func (s *HTTPSource) GetConfig(ctx context.Context, ref ConfigRef) (string, error) {
	token, err := s.accessToken(ctx)
	if err != nil {
		return "", err
	}
	query := "group=" + escape(ref.Group) + "&dataId=" + escape(ref.DataID)
	if s.conn.Namespace != "" {
		query += "&tenant=" + escape(s.conn.Namespace)
	}
	if token != nil {
		query += "&accessToken=" + escape(*token)
	}
	u := s.base
	u.Path, u.RawQuery = "/nacos/v1/cs/configs", query
	body, err := s.request(ctx, http.MethodGet, u, nil, "get config")
	if err != nil {
		return "", fmt.Errorf("%s/%s: %w", ref.DataID, ref.Group, err)
	}
	return strings.ToValidUTF8(string(body), "\uFFFD"), nil
}

func (s *HTTPSource) accessToken(ctx context.Context) (*string, error) {
	if s.conn.Username == "" && s.conn.Password == "" {
		return nil, nil
	}
	select {
	case s.login <- struct{}{}:
		defer func() { <-s.login }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if s.token != nil {
		return s.token, nil
	}
	u := s.base
	u.Path = "/nacos/v1/auth/login"
	form := "username=" + escape(s.conn.Username) + "&password=" + escape(s.conn.Password)
	body, err := s.request(ctx, http.MethodPost, u, strings.NewReader(form), "nacos login")
	if err != nil {
		return nil, err
	}
	raw, err := decode(body, JSON)
	if err != nil {
		return nil, fmt.Errorf("decode Nacos login response: %w", err)
	}
	response, _ := raw.(map[string]any)
	token, ok := response["accessToken"].(string)
	if !ok {
		return nil, fmt.Errorf("Nacos login response has no accessToken")
	}
	s.token = &token
	return s.token, nil
}

func (s *HTTPSource) request(ctx context.Context, method string, endpoint url.URL, body io.Reader, operation string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("%s request: %w", operation, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := s.client.Do(request)
	if err != nil {
		// net/http's URL wrapper includes the access token in its error string.
		var requestError *url.Error
		if errors.As(err, &requestError) {
			err = requestError.Err
		}
		return nil, fmt.Errorf("%s request: %w", operation, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &HTTPError{Operation: operation, StatusCode: response.StatusCode}
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("%s body: %w", operation, err)
	}
	return data, nil
}

func escape(text string) string {
	return strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}
