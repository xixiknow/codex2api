package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex2api/database"
	"github.com/gin-gonic/gin"
)

const viewerTestAdminSecret = "viewer-test-admin-secret"

func newViewerTestEngine(t *testing.T) (*gin.Engine, *Handler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.New("sqlite", filepath.Join(t.TempDir(), "viewer.db"))
	if err != nil {
		t.Fatalf("database.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := &Handler{db: db, adminSecretEnv: viewerTestAdminSecret}
	r := gin.New()
	h.RegisterRoutes(r)
	return r, h
}

func viewerTestRequest(r *gin.Engine, method, path, key, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if key != "" {
		req.Header.Set("X-Admin-Key", key)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func createViewerKeyForTest(t *testing.T, r *gin.Engine, name string) (int64, string) {
	t.Helper()
	rec := viewerTestRequest(r, http.MethodPost, "/api/admin/viewer-keys", viewerTestAdminSecret, `{"name":"`+name+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create viewer key status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(resp.Key, viewerKeyPrefix) || resp.ID <= 0 {
		t.Fatalf("unexpected create response %s", rec.Body.String())
	}
	return resp.ID, resp.Key
}

// concretePath 把路由模板里的 :param / *param 替换成占位值。
func concretePath(fullPath string) string {
	parts := strings.Split(fullPath, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") || strings.HasPrefix(part, "*") {
			parts[i] = "1"
		}
	}
	return strings.Join(parts, "/")
}

func TestViewerAllowlistIsGetOnlyAndRegistered(t *testing.T) {
	r, _ := newViewerTestEngine(t)
	registered := make(map[string]bool)
	for _, route := range r.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for entry := range viewerAllowedRoutes {
		if !strings.HasPrefix(entry, "GET ") {
			t.Errorf("allowlist entry %q is not GET", entry)
		}
		if !registered[entry] {
			t.Errorf("allowlist entry %q does not match any registered route", entry)
		}
	}
}

func TestViewerDeniedOnEveryNonAllowlistedRoute(t *testing.T) {
	r, _ := newViewerTestEngine(t)
	_, viewerKey := createViewerKeyForTest(t, r, "reader")

	checked := 0
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/api/admin/") {
			continue
		}
		if _, allowed := viewerAllowedRoutes[route.Method+" "+route.Path]; allowed {
			continue
		}
		if route.Path == "/api/admin/bootstrap" || route.Path == "/api/admin/bootstrap-status" {
			continue // 注册在鉴权组之外
		}
		rec := viewerTestRequest(r, route.Method, concretePath(route.Path), viewerKey, "")
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"read_only"`) {
			t.Errorf("%s %s: viewer got status=%d body=%s, want 403 read_only", route.Method, route.Path, rec.Code, rec.Body.String())
		}
		checked++
	}
	if checked < 100 {
		t.Fatalf("only %d routes checked, route registration looks wrong", checked)
	}
	for _, path := range []string{
		"/api/admin/accounts/1/test", "/api/admin/accounts/export", "/api/admin/accounts/1/auth-json",
		"/api/admin/keys", "/api/admin/proxies", "/api/admin/viewer-keys", "/api/admin/system/update",
	} {
		if rec := viewerTestRequest(r, http.MethodGet, path, viewerKey, ""); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s: viewer status=%d, want 403", path, rec.Code)
		}
	}
}

func TestViewerAccountsListRequiresPageView(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &Handler{}
	r.Use(func(c *gin.Context) { setAdminRole(c, adminRoleViewer, "reader") })
	r.Use(h.viewerRouteGuard())
	r.GET("/api/admin/accounts", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/api/admin/keys", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	for path, want := range map[string]int{
		"/api/admin/accounts":           http.StatusForbidden,
		"/api/admin/accounts?view=lite": http.StatusForbidden,
		"/api/admin/accounts?view=page": http.StatusOK,
		"/api/admin/keys":               http.StatusForbidden,
		"/api/admin/keys?view=lite":     http.StatusOK,
	} {
		if rec := viewerTestRequest(r, http.MethodGet, path, "", ""); rec.Code != want {
			t.Errorf("GET %s status=%d, want %d", path, rec.Code, want)
		}
	}
}

func TestViewerKeyLifecycleAndAdminStillWorks(t *testing.T) {
	r, h := newViewerTestEngine(t)

	rec := viewerTestRequest(r, http.MethodGet, "/api/admin/me", viewerTestAdminSecret, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"admin"`) {
		t.Fatalf("admin /me status=%d body=%s", rec.Code, rec.Body.String())
	}

	id, viewerKey := createViewerKeyForTest(t, r, "share@example.com")

	rec = viewerTestRequest(r, http.MethodGet, "/api/admin/me", viewerKey, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"viewer"`) {
		t.Fatalf("viewer /me status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "share@example.com") {
		t.Fatalf("viewer /me leaked unmasked email: %s", rec.Body.String())
	}

	// 管理列表不返回明文或哈希
	rec = viewerTestRequest(r, http.MethodGet, "/api/admin/viewer-keys", viewerTestAdminSecret, "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), viewerKey) || strings.Contains(rec.Body.String(), hashViewerKey(viewerKey)) {
		t.Fatalf("list viewer keys status=%d body=%s", rec.Code, rec.Body.String())
	}
	keys, err := h.db.ListAdminViewerKeys(t.Context())
	if err != nil || len(keys) != 1 || !keys[0].LastUsedAt.Valid {
		t.Fatalf("keys=%+v err=%v, want one key with last_used_at", keys, err)
	}

	// viewer 不能管理密钥
	if rec := viewerTestRequest(r, http.MethodPost, "/api/admin/viewer-keys", viewerKey, `{"name":"x"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create key status=%d", rec.Code)
	}
	if rec := viewerTestRequest(r, http.MethodDelete, "/api/admin/viewer-keys/1", viewerKey, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer revoke key status=%d", rec.Code)
	}

	rec = viewerTestRequest(r, http.MethodDelete, "/api/admin/viewer-keys/"+itoa64(id), viewerTestAdminSecret, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 吊销后缓存立即失效
	if rec := viewerTestRequest(r, http.MethodGet, "/api/admin/me", viewerKey, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key status=%d, want 401", rec.Code)
	}
	if rec := viewerTestRequest(r, http.MethodDelete, "/api/admin/viewer-keys/99999", viewerTestAdminSecret, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("revoke missing status=%d, want 404", rec.Code)
	}
	if rec := viewerTestRequest(r, http.MethodGet, "/api/admin/me", viewerTestAdminSecret, ""); rec.Code != http.StatusOK {
		t.Fatalf("admin after revoke status=%d", rec.Code)
	}
}

func TestViewerKeyFailuresAreThrottled(t *testing.T) {
	r, h := newViewerTestEngine(t)
	h.adminAuthThrottle = newAdminAuthThrottle()
	bogus := viewerKeyPrefix + "not-a-real-key"
	var last int
	for i := 0; i < adminAuthMaxFailures; i++ {
		last = viewerTestRequest(r, http.MethodGet, "/api/admin/me", bogus, "").Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after %d bad viewer keys status=%d, want 429", adminAuthMaxFailures, last)
	}
}

// 登录页会不带密钥轮询 /health；这些请求不能把本机 IP 锁住。
func TestMissingKeyDoesNotTripThrottle(t *testing.T) {
	r, h := newViewerTestEngine(t)
	h.adminAuthThrottle = newAdminAuthThrottle()
	for i := 0; i < adminAuthMaxFailures*3; i++ {
		if code := viewerTestRequest(r, http.MethodGet, "/api/admin/me", "", "").Code; code != http.StatusUnauthorized {
			t.Fatalf("keyless request %d status=%d, want 401", i, code)
		}
	}
	if code := viewerTestRequest(r, http.MethodGet, "/api/admin/me", viewerTestAdminSecret, "").Code; code != http.StatusOK {
		t.Fatalf("admin key after keyless polling status=%d, want 200", code)
	}
}

func TestViewerResponseRedactor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fixture := map[string]interface{}{
		"accounts": []interface{}{
			map[string]interface{}{
				"id":                      7,
				"name":                    "alice.smith@gmail.com",
				"email":                   "alice.smith@gmail.com",
				"account_email":           "bo@corp.io",
				"access_token":            "at-secret",
				"refresh_token":           "rt-secret",
				"id_token":                "idt",
				"access_token_type":       "bearer",
				"input_tokens":            123,
				"github_token_configured": true,
				"credentials":             map[string]interface{}{"session_token": "s", "plan": "plus"},
				"proxy_url":               "socks5://user:pass@1.2.3.4:1080",
				"proxy_urls":              []interface{}{"http://u:p@h:1"},
				"claude_base_url":         "https://relay.example",
				"custom_headers":          map[string]interface{}{"X-Key": "v"},
				"note":                    "via http://bob:hunter2@proxy.local:8080/ ok",
			},
		},
		"admin_secret":        "s3cr3t",
		"image_s3_access_key": "AKIA",
		"image_s3_secret_key": "xyz",
		"api_key":             "sk-1",
		"raw_key":             "sk-2",
		"api_key_masked":      "sk-****",
		"auth_json":           "{}",
		"resin_url":           "https://resin",
		"total_tokens":        999,
	}

	h := &Handler{}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if c.Query("as") == "viewer" {
			setAdminRole(c, adminRoleViewer, "reader")
		}
	})
	r.Use(h.viewerResponseRedactor())
	r.GET("/fixture", func(c *gin.Context) { c.JSON(http.StatusOK, fixture) })

	adminRec := viewerTestRequest(r, http.MethodGet, "/fixture", "", "")
	if !strings.Contains(adminRec.Body.String(), "at-secret") {
		t.Fatalf("admin response must not be redacted: %s", adminRec.Body.String())
	}

	rec := viewerTestRequest(r, http.MethodGet, "/fixture?as=viewer", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, leaked := range []string{
		"at-secret", "rt-secret", "idt", "bearer", "s3cr3t", "AKIA", "xyz", "sk-1", "sk-2",
		"user:pass", "u:p@", "hunter2", "relay.example", "X-Key", "https://resin", "alice.smith", "session_token",
		`"bo@`,
	} {
		if strings.Contains(body, leaked) {
			t.Errorf("viewer response leaked %q: %s", leaked, body)
		}
	}

	var got map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	acct := got["accounts"].([]interface{})[0].(map[string]interface{})
	if acct["email"] != "al***@gmail.com" || acct["name"] != "al***@gmail.com" {
		t.Errorf("email masking wrong: email=%v name=%v", acct["email"], acct["name"])
	}
	if acct["account_email"] != "b***@corp.io" {
		t.Errorf("account_email=%v", acct["account_email"])
	}
	if acct["note"] != "via http://proxy.local:8080/ ok" {
		t.Errorf("note=%v", acct["note"])
	}
	if acct["input_tokens"] != float64(123) || got["total_tokens"] != float64(999) {
		t.Errorf("numeric token stats must be kept: %v %v", acct["input_tokens"], got["total_tokens"])
	}
	if acct["github_token_configured"] != true {
		t.Errorf("boolean flags must be kept")
	}
	if got["api_key_masked"] != "sk-****" {
		t.Errorf("api_key_masked=%v", got["api_key_masked"])
	}
	if creds, ok := acct["credentials"].(map[string]interface{}); !ok || creds["plan"] != "plus" {
		t.Errorf("nested object should be walked, credentials=%v", acct["credentials"])
	}
}

func itoa64(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestMaskViewerIP(t *testing.T) {
	cases := map[string]string{
		"203.0.113.42":        "203.0.*.*",
		"2001:db8:85a3::8a2e": "2001:db8:*",
		"":                    "",
		"not-an-ip":           "*",
	}
	for in, want := range cases {
		if got := maskViewerIP(in); got != want {
			t.Errorf("maskViewerIP(%q) = %q, want %q", in, got, want)
		}
	}
	out, err := redactViewerJSON([]byte(`{"logs":[{"client_ip":"198.51.100.7","input_tokens":12}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"client_ip":"198.51.*.*"`) || !strings.Contains(string(out), `"input_tokens":12`) {
		t.Fatalf("unexpected redaction: %s", out)
	}
}
