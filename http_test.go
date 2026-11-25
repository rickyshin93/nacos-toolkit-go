package nacostoolkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPPublicNamespace(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/nacos/v1/cs/configs" ||
			r.URL.RawQuery != "group=DEFAULT_GROUP&dataId=app.yml" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "env: ${DEPLOY_ENV}")
	}))
	defer server.Close()

	manager, err := New(Connection{ServerAddr: strings.TrimPrefix(server.URL, "http://")})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Get(context.Background(), []ConfigRef{{"app.yml", "DEFAULT_GROUP"}}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	assertValue(t, result.Config, wantJSON(t, `{"env":""}`))
	if result.Raw != nil || requests.Load() != 1 {
		t.Fatalf("raw = %v, requests = %d", result.Raw, requests.Load())
	}
}

func TestHTTPBatchAndCaches(t *testing.T) {
	const token = "session +雪&?"
	bodies := map[string]string{
		"common app/配置.yml": "baseOnly: keep\nPORT: 8848\nold: {lost: true}\nDEPLOY_ENV: original\nx: common",
		"app.json":          "x: app\nold: {current: true}\napp:\n  port: ${PORT}\n  env: ${DEPLOY_ENV}\n  host: from-base\ncors: {whitelist: 'a, b'}",
		"override.json":     `{"app":{"url":"http://${x}/${DEPLOY_ENV}","shadowed":"${app.host}","flag":false},"baseRef":"${baseOnly}","cors":{"whitelist":["c"]}}`,
		"bad.json":          "port: 123",
	}
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/nacos/v1/auth/login" {
			requests = append(requests, "login")
			body, err := io.ReadAll(r.Body)
			if err != nil || r.Method != http.MethodPost ||
				r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" ||
				string(body) != "username=%E6%B5%8B%20%E8%AF%95%2B&password=p%26%3D%20%25" {
				t.Errorf("unexpected login method, content type or form: %v", err)
			}
			fmt.Fprintf(w, `{"accessToken":%q,"tokenTtl":1}`, token)
			return
		}
		params := r.URL.Query()
		id := params.Get("dataId")
		requests = append(requests, id)
		if r.Method != http.MethodGet || r.URL.Path != "/nacos/v1/cs/configs" ||
			params.Get("group") != "team & +" || params.Get("tenant") != "ns/测试" ||
			params.Get("accessToken") != token {
			t.Errorf("unexpected config request %s %s", r.Method, r.URL.RequestURI())
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if id == "common app/配置.yml" {
			const want = "group=team%20%26%20%2B&dataId=common%20app%2F%E9%85%8D%E7%BD%AE.yml&tenant=ns%2F%E6%B5%8B%E8%AF%95&accessToken=session%20%2B%E9%9B%AA%26%3F"
			if r.URL.RawQuery != want {
				t.Errorf("query = %s, want %s", r.URL.RawQuery, want)
			}
		}
		if body, ok := bodies[id]; ok {
			fmt.Fprint(w, body)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	manager, err := New(Connection{server.URL, "ns/测试", "测 试+", "p&= %"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref := func(id string) ConfigRef { return ConfigRef{id, "team & +"} }
	base := []ConfigRef{ref("common app/配置.yml"), ref("app.json")}
	override := ref("override.json")
	wantRaw := wantJSON(t, `{"baseOnly":"keep","PORT":8848,"old":{"current":true},"DEPLOY_ENV":"original","x":"app","app":{"port":"${PORT}","env":"${DEPLOY_ENV}","host":"from-base"},"cors":{"whitelist":"a, b"}}`)
	wantConfig := wantJSON(t, `{"x":"app","old":{"current":true},"app":{"port":8848,"env":"ns/测试","host":"from-base","flag":false,"url":"http://app/ns/测试","shadowed":"${app.host}"},"cors":{"whitelist":["c"]},"baseRef":"keep"}`)
	result, err := manager.Get(ctx, base, &override, true)
	if err != nil {
		t.Fatal(err)
	}
	assertValue(t, result.Raw, wantRaw)
	assertValue(t, result.Config, wantConfig)
	result.Raw["app"].(map[string]any)["port"] = "changed"
	result.Config.(map[string]any)["x"] = "changed"

	missing := ref("missing.yml")
	cached, err := manager.Get(ctx, []ConfigRef{missing}, &missing, false)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Raw != nil {
		t.Fatalf("raw leaked from non-debug cache hit: %v", cached.Raw)
	}
	assertValue(t, cached.Config, wantConfig)
	assertValue(t, manager.Raw(), wantRaw)
	manager.ClearCache()
	if manager.Raw() != nil {
		t.Fatal("raw cache was not cleared")
	}

	bad := ref("bad.json")
	again, err := manager.Get(ctx, base, &bad, true)
	if err != nil {
		t.Fatal(err)
	}
	assertValue(t, again.Config, wantJSON(t, `{"x":"app","old":{"current":true},"app":{"port":8848,"env":"ns/测试","host":"from-base"},"cors":{"whitelist":["a","b"]}}`))
	mu.Lock()
	defer mu.Unlock()
	assertValue(t, requests, []string{"login", "common app/配置.yml", "app.json", "override.json", "common app/配置.yml", "app.json", "bad.json"})
}

func TestHTTPAuthErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		login    string
		status   int
		errorHas string
	}{
		{"login HTTP error", "", 403, "HTTP 403"},
		{"invalid JSON", "no", 200, "decode Nacos login response"},
		{"missing token", `{"tokenTtl":4}`, 200, "no accessToken"},
		{"wrong token type", `{"accessToken":true}`, 200, "no accessToken"},
		{"wrong token case", `{"AccessToken":"value"}`, 200, "no accessToken"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls, configCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/nacos/v1/cs/configs" {
					configCalls.Add(1)
					fmt.Fprint(w, "ok: true")
					return
				}
				if r.URL.Path != "/nacos/v1/auth/login" || r.FormValue("username") != "" || r.FormValue("password") != "test-pass" {
					t.Errorf("expected login with password-only credentials")
				}
				if calls.Add(1) == 1 {
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.login)
					return
				}
				fmt.Fprint(w, `{"accessToken":""}`)
			}))
			defer server.Close()
			source, err := NewHTTPSource(Connection{ServerAddr: server.URL, Password: "test-pass"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = source.GetConfig(context.Background(), ConfigRef{"app.yml", "DEFAULT_GROUP"})
			if err == nil || !strings.Contains(err.Error(), tc.errorHas) {
				t.Fatalf("error = %v, want %s", err, tc.errorHas)
			}
			if tc.status != 200 {
				var status *HTTPError
				if !errors.As(err, &status) || status.StatusCode != tc.status {
					t.Fatalf("typed error = %v", err)
				}
			}
			if calls.Load() != 1 || configCalls.Load() != 0 {
				t.Fatal("login failure must stop config fetch")
			}
			body, err := source.GetConfig(context.Background(), ConfigRef{"app.yml", "DEFAULT_GROUP"})
			if err != nil || body != "ok: true" || calls.Load() != 2 {
				t.Fatalf("fetch after failed login = %q, %v, login calls = %d", body, err, calls.Load())
			}
		})
	}
}

func TestHTTPConcurrentAuthAndCanceledWait(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nacos/v1/auth/login":
			if loginCalls.Add(1) == 1 {
				close(started)
			}
			select {
			case <-release:
				fmt.Fprint(w, `{"accessToken":"test-token"}`)
			case <-r.Context().Done():
			}
		case "/nacos/v1/cs/configs":
			if r.URL.Query().Get("accessToken") != "test-token" {
				t.Errorf("missing auth token")
			}
			fmt.Fprint(w, "ok: true")
		}
	}))
	defer server.Close()

	source, err := NewHTTPSource(Connection{ServerAddr: server.URL, Username: "test-user"})
	if err != nil {
		t.Fatal(err)
	}
	ref := ConfigRef{"x.yml", "DEFAULT_GROUP"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if body, err := source.GetConfig(ctx, ref); err != nil || body != "ok: true" {
				t.Errorf("GetConfig = %q, %v", body, err)
			}
		})
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("login did not start")
	}
	waitCtx, cancelWait := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancelWait()
	if _, err := source.GetConfig(waitCtx, ref); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("wait error = %v", err)
	}
	close(release)
	wg.Wait()
	if loginCalls.Load() != 1 {
		t.Fatalf("login count = %d", loginCalls.Load())
	}
}

func TestHTTPFailureKeepsToken(t *testing.T) {
	var configCalls, loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			loginCalls.Add(1)
			fmt.Fprint(w, `{"accessToken":""}`)
			return
		}
		if !r.URL.Query().Has("accessToken") {
			t.Errorf("a successful empty token must still be sent")
		}
		if configCalls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "ok: true")
	}))
	defer server.Close()

	source, err := NewHTTPSource(Connection{ServerAddr: server.URL, Username: "test-user"})
	if err != nil {
		t.Fatal(err)
	}
	ref := ConfigRef{"x.yml", "DEFAULT_GROUP"}
	if _, err := source.GetConfig(context.Background(), ref); err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("config error = %v", err)
	}
	if body, err := source.GetConfig(context.Background(), ref); err != nil || body != "ok: true" {
		t.Fatalf("GetConfig after 401 = %q, %v", body, err)
	}
	if loginCalls.Load() != 1 || configCalls.Load() != 2 {
		t.Fatal("config requests unexpectedly retried or refreshed the token")
	}
}

func TestHTTPConfigCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"accessToken":"token-never-in-errors"}`)
			return
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	source, err := NewHTTPSource(Connection{ServerAddr: server.URL, Username: "test-user"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := source.GetConfig(ctx, ConfigRef{DataID: "x.yml", Group: "test"})
		finished <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("config request did not start")
	}
	cancel()
	err = <-finished
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "token-never-in-errors") {
		t.Fatal("context error was lost or contained the access token")
	}
}
