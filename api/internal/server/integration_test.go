package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/thiagolago1/featherflags/api/internal/server"
	"github.com/thiagolago1/featherflags/api/internal/store"
)

const adminToken = "integration-test-token"

// newTestServer skips unless TEST_DATABASE_URL points at a Postgres instance
// (docker compose up -d db locally; a service container in CI).
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration tests")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ts := httptest.NewServer(server.New(st, adminToken))
	t.Cleanup(ts.Close)
	return ts
}

func adminReq(t *testing.T, ts *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func decode[T any](t *testing.T, res *http.Response) T {
	t.Helper()
	defer res.Body.Close()
	var v T
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func evaluate(t *testing.T, ts *httptest.Server, apiKey, userID string, attrs map[string]string) map[string]bool {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"userId": userID, "attributes": attrs})
	req, _ := http.NewRequest("POST", ts.URL+"/v1/evaluate", bytes.NewReader(body))
	req.Header.Set("X-API-Key", apiKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("evaluate: HTTP %d", res.StatusCode)
	}
	out := decode[struct {
		Flags map[string]bool `json:"flags"`
	}](t, res)
	return out.Flags
}

func TestFullFlagLifecycle(t *testing.T) {
	ts := newTestServer(t)

	// Create project → three env keys.
	res := adminReq(t, ts, "POST", "/admin/projects", map[string]string{"name": "it-lifecycle"})
	if res.StatusCode != 201 {
		t.Fatalf("create project: HTTP %d", res.StatusCode)
	}
	proj := decode[store.Project](t, res)
	if len(proj.APIKeys) != 3 {
		t.Fatalf("want 3 api keys, got %d", len(proj.APIKeys))
	}
	keys := map[string]string{}
	for _, k := range proj.APIKeys {
		keys[k.Env] = k.Key
	}

	// Create flag → born disabled in all envs.
	res = adminReq(t, ts, "POST", "/admin/projects/"+proj.ID+"/flags", map[string]string{"key": "it-flag"})
	if res.StatusCode != 201 {
		t.Fatalf("create flag: HTTP %d", res.StatusCode)
	}
	flag := decode[store.Flag](t, res)
	if len(flag.Rules) != 3 {
		t.Fatalf("want 3 rules, got %d", len(flag.Rules))
	}
	if on := evaluate(t, ts, keys["development"], "u1", nil)["it-flag"]; on {
		t.Error("new flag must evaluate false")
	}

	// Enable in development only.
	res = adminReq(t, ts, "PATCH", "/admin/flags/"+flag.ID+"/rules/development", map[string]any{"enabled": true})
	if res.StatusCode != 204 {
		t.Fatalf("update rule: HTTP %d", res.StatusCode)
	}
	if on := evaluate(t, ts, keys["development"], "u1", nil)["it-flag"]; !on {
		t.Error("development must be true after enable")
	}
	for _, env := range []string{"staging", "production"} {
		if on := evaluate(t, ts, keys[env], "u1", nil)["it-flag"]; on {
			t.Errorf("%s must stay false", env)
		}
	}

	// Conditions round-trip through Postgres JSONB.
	res = adminReq(t, ts, "PATCH", "/admin/flags/"+flag.ID+"/rules/development", map[string]any{
		"conditions": []map[string]any{{"attr": "appVersion", "op": "semver_gte", "value": "2.0.0"}},
	})
	if res.StatusCode != 204 {
		t.Fatalf("set conditions: HTTP %d", res.StatusCode)
	}
	if on := evaluate(t, ts, keys["development"], "u1", map[string]string{"appVersion": "2.1.0"})["it-flag"]; !on {
		t.Error("2.1.0 >= 2.0.0 must be true")
	}
	if on := evaluate(t, ts, keys["development"], "u1", map[string]string{"appVersion": "1.9.0"})["it-flag"]; on {
		t.Error("1.9.0 >= 2.0.0 must be false")
	}

	// Archive wins over everything.
	res = adminReq(t, ts, "POST", "/admin/flags/"+flag.ID+"/archive", nil)
	if res.StatusCode != 204 {
		t.Fatalf("archive: HTTP %d", res.StatusCode)
	}
	if on := evaluate(t, ts, keys["development"], "u1", map[string]string{"appVersion": "2.1.0"})["it-flag"]; on {
		t.Error("archived flag must evaluate false")
	}
}

func TestAuth(t *testing.T) {
	ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/admin/projects", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Errorf("bad admin token: want 401, got %d", res.StatusCode)
	}

	body := strings.NewReader(`{"userId":"u1"}`)
	req, _ = http.NewRequest("POST", ts.URL+"/v1/evaluate", body)
	req.Header.Set("X-API-Key", "ff_dev_nonexistent")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Errorf("bad api key: want 401, got %d", res.StatusCode)
	}
}

func TestSSEStreamReceivesChange(t *testing.T) {
	ts := newTestServer(t)

	proj := decode[store.Project](t,
		adminReq(t, ts, "POST", "/admin/projects", map[string]string{"name": "it-sse"}))
	flag := decode[store.Flag](t,
		adminReq(t, ts, "POST", "/admin/projects/"+proj.ID+"/flags", map[string]string{"key": "sse-flag"}))
	var devKey string
	for _, k := range proj.APIKeys {
		if k.Env == "development" {
			devKey = k.Key
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/v1/stream?apiKey=%s", ts.URL, devKey), nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("stream: HTTP %d", res.StatusCode)
	}

	events := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "event: ") {
				events <- strings.TrimPrefix(line, "event: ")
			}
		}
	}()

	// Give the subscription a moment, then write.
	time.Sleep(200 * time.Millisecond)
	adminReq(t, ts, "PATCH", "/admin/flags/"+flag.ID+"/rules/development",
		map[string]any{"enabled": true}).Body.Close()

	select {
	case ev := <-events:
		if ev != "change" {
			t.Errorf("want change event, got %q", ev)
		}
	case <-ctx.Done():
		t.Fatal("no SSE event within 10s of admin write")
	}
}
