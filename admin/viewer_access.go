package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codex2api/database"
	"github.com/codex2api/security"
	"github.com/gin-gonic/gin"
)

// 只读管理角色（viewer）：持只读密钥登录后台，可查看仪表盘/账号池/用量，
// 不能执行任何写操作或触发上游请求，响应中的令牌/代理凭据/系统秘密一律剔除，邮箱打码。
//
// 放行策略是默认拒绝：只有 viewerAllowedRoutes 中登记的 "METHOD 路由模板" 可访问。
// 新增路由默认对 viewer 不可见，无需改动本文件。

const (
	viewerKeyPrefix        = "tpv_"
	viewerKeyCacheTTL      = 30 * time.Second
	viewerKeyTouchInterval = time.Minute
	viewerKeyNameMaxLen    = 64

	adminRoleAdmin  = "admin"
	adminRoleViewer = "viewer"

	ctxAdminRole       = "admin_role"
	ctxAdminViewerName = "admin_viewer_name"
)

// viewerAllowedRoutes 是 viewer 可访问路由的白名单。值为可选的附加校验（nil 表示无条件放行）。
// 只允许 GET；每条都已确认 handler 只读数据库/内存，不写状态、不调用上游。
var viewerAllowedRoutes = map[string]func(*gin.Context) bool{
	"GET /api/admin/health": nil,
	"GET /api/admin/me":     nil,
	"GET /api/admin/stats":  nil,
	// 旧全量视图会触发 TriggerUsageProbeAsync/TriggerRecoveryProbeAsync（上游探测），
	// viewer 只能用前端实际使用的分页视图 view=page。
	"GET /api/admin/accounts":                  viewerAccountsPageViewOnly,
	"GET /api/admin/accounts/analysis":         nil,
	"GET /api/admin/accounts/page-stats":       nil,
	"GET /api/admin/accounts/live":             nil,
	"GET /api/admin/accounts/:id":              nil,
	"GET /api/admin/accounts/health-bars":      nil,
	"GET /api/admin/accounts/event-trend":      nil,
	"GET /api/admin/accounts/:id/usage":        nil,
	"GET /api/admin/usage/stats":               nil,
	"GET /api/admin/usage/chart-data":          nil,
	"GET /api/admin/usage/logs":                nil,
	"GET /api/admin/usage/logs/error-summary":  nil,
	"GET /api/admin/usage/api-keys":            nil,
	"GET /api/admin/settings":                  nil,
	"GET /api/admin/settings/visible-channels": nil,
	"GET /api/admin/models":                    nil,
	"GET /api/admin/account-groups":            nil,
	// 仪表盘/用量页的 Key 筛选下拉只需名称：lite 视图里 key 已打码，raw_key 由脱敏器剔除；
	// 完整视图会聚合窗口费用，viewer 用不到。
	"GET /api/admin/keys": viewerKeysLiteViewOnly,
	// 账号卡片上的只读展示数据：渠道倍率快照与智力检测条，均只读数据库。
	"GET /api/admin/channel-monitors/billing-rates": nil,
	"GET /api/admin/intelligence-check/bars":        nil,
}

func viewerAccountsPageViewOnly(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.Query("view")), "page")
}

func viewerKeysLiteViewOnly(c *gin.Context) bool {
	return strings.EqualFold(strings.TrimSpace(c.Query("view")), "lite")
}

// ==================== 角色上下文 ====================

func setAdminRole(c *gin.Context, role, name string) {
	c.Set(ctxAdminRole, role)
	if name != "" {
		c.Set(ctxAdminViewerName, name)
	}
}

func isViewer(c *gin.Context) bool {
	return c.GetString(ctxAdminRole) == adminRoleViewer
}

// ==================== 密钥生成与查找 ====================

func generateViewerKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return viewerKeyPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashViewerKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func viewerKeyDisplayPrefix(key string) string {
	if len(key) > len(viewerKeyPrefix)+6 {
		return key[:len(viewerKeyPrefix)+6]
	}
	return key
}

type viewerKeyCacheEntry struct {
	key       database.AdminViewerKey
	expiresAt time.Time
}

// viewerKeyCache 以哈希为键缓存已验证的只读密钥，零值可用。
type viewerKeyCache struct {
	mu        sync.Mutex
	entries   map[string]viewerKeyCacheEntry
	lastTouch map[int64]time.Time
}

func (vc *viewerKeyCache) get(hash string, now time.Time) (database.AdminViewerKey, bool) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	entry, ok := vc.entries[hash]
	if !ok || !now.Before(entry.expiresAt) {
		delete(vc.entries, hash)
		return database.AdminViewerKey{}, false
	}
	return entry.key, true
}

func (vc *viewerKeyCache) put(hash string, key database.AdminViewerKey, now time.Time) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	if vc.entries == nil {
		vc.entries = make(map[string]viewerKeyCacheEntry)
	}
	for h, entry := range vc.entries {
		if !now.Before(entry.expiresAt) {
			delete(vc.entries, h)
		}
	}
	vc.entries[hash] = viewerKeyCacheEntry{key: key, expiresAt: now.Add(viewerKeyCacheTTL)}
}

func (vc *viewerKeyCache) invalidate(hash string) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	delete(vc.entries, hash)
}

// shouldTouch 返回该密钥是否需要刷新 last_used_at（每分钟至多一次）。
func (vc *viewerKeyCache) shouldTouch(id int64, now time.Time) bool {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	if vc.lastTouch == nil {
		vc.lastTouch = make(map[int64]time.Time)
	}
	if last, ok := vc.lastTouch[id]; ok && now.Sub(last) < viewerKeyTouchInterval {
		return false
	}
	vc.lastTouch[id] = now
	return true
}

// lookupViewerKey 校验只读密钥明文，返回未吊销的记录；无效时返回 nil。
func (h *Handler) lookupViewerKey(ctx context.Context, plaintext string) *database.AdminViewerKey {
	if h == nil || h.db == nil || !strings.HasPrefix(plaintext, viewerKeyPrefix) {
		return nil
	}
	hash := hashViewerKey(plaintext)
	now := time.Now()
	key, ok := h.viewerKeys.get(hash, now)
	if !ok {
		readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		found, err := h.db.FindAdminViewerKeyByHash(readCtx, hash)
		cancel()
		if err != nil || found == nil {
			return nil
		}
		key = *found
		h.viewerKeys.put(hash, key, now)
	}
	if h.viewerKeys.shouldTouch(key.ID, now) {
		touchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = h.db.TouchAdminViewerKey(touchCtx, key.ID, now)
		cancel()
	}
	return &key
}

// authenticateViewerKey 在管理员密钥比对失败后尝试只读密钥；成功时写入角色与审计日志。
func (h *Handler) authenticateViewerKey(c *gin.Context, candidate string) bool {
	key := h.lookupViewerKey(c.Request.Context(), candidate)
	if key == nil {
		return false
	}
	setAdminRole(c, adminRoleViewer, key.Name)
	security.SecurityAuditLog("ADMIN_VIEWER_ACCESS", fmt.Sprintf("key=%s path=%s ip=%s method=%s",
		security.SanitizeLog(key.Name), c.Request.URL.Path, security.SanitizeLog(c.ClientIP()), c.Request.Method))
	return true
}

// ==================== 路由守卫 ====================

func viewerRouteAllowed(c *gin.Context) bool {
	check, ok := viewerAllowedRoutes[c.Request.Method+" "+c.FullPath()]
	if !ok {
		return false
	}
	return check == nil || check(c)
}

func (h *Handler) viewerRouteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isViewer(c) && !viewerRouteAllowed(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "只读账号无权执行此操作",
				"code":  "read_only",
			})
			return
		}
		c.Next()
	}
}

// ==================== 响应脱敏 ====================

type viewerBufferedWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *viewerBufferedWriter) Write(data []byte) (int, error) { return w.body.Write(data) }

func (w *viewerBufferedWriter) WriteString(s string) (int, error) { return w.body.WriteString(s) }

// viewerResponseRedactor 仅对 viewer 生效：缓冲 JSON 响应，递归剔除秘密字段、邮箱打码、去掉 URL 中的凭据。
func (h *Handler) viewerResponseRedactor() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isViewer(c) {
			c.Next()
			return
		}
		original := c.Writer
		buffered := &viewerBufferedWriter{ResponseWriter: original}
		c.Writer = buffered
		c.Next()
		c.Writer = original

		body := buffered.body.Bytes()
		if len(body) > 0 && strings.Contains(strings.ToLower(original.Header().Get("Content-Type")), "json") {
			if redacted, err := redactViewerJSON(body); err == nil {
				body = redacted
			} else {
				// 无法解析的 JSON 不能确认安全，宁可不返回。
				body = []byte(`{"error":"响应脱敏失败","code":"read_only"}`)
			}
		} else if len(body) > 0 {
			body = []byte(redactViewerString(string(body)))
		}
		if len(body) > 0 {
			_, _ = original.Write(body)
		}
	}
}

func redactViewerJSON(body []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload interface{}
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	return json.Marshal(redactViewerValue(payload))
}

var (
	viewerDropExactKeys = map[string]struct{}{
		"raw_key": {}, "api_key": {}, "admin_secret": {}, "auth_json": {},
		"custom_headers": {}, "balance_query_url": {},
	}
	// 键名包含以下片段即视为敏感；值为数字/布尔/null 时保留（如 input_tokens、has_secret），
	// 其余类型（字符串、对象、数组）一律剔除。
	viewerDropKeyFragments = []string{"token", "secret", "password", "proxy_url", "resin_url", "base_url"}
	viewerS3KeyPattern     = regexp.MustCompile(`s3_.*key`)
	viewerEmailPattern     = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}`)
	viewerURLUserinfo      = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://)[^/?#@\s"]+@`)
)

func viewerSensitiveKey(key string, value interface{}) bool {
	lower := strings.ToLower(key)
	if _, ok := viewerDropExactKeys[lower]; ok {
		return true
	}
	matched := viewerS3KeyPattern.MatchString(lower)
	for _, fragment := range viewerDropKeyFragments {
		if matched {
			break
		}
		matched = strings.Contains(lower, fragment)
	}
	if !matched {
		return false
	}
	switch value.(type) {
	case nil, bool, json.Number, float64:
		return false
	}
	return true
}

func redactViewerValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, child := range v {
			if viewerSensitiveKey(key, child) {
				continue
			}
			lower := strings.ToLower(key)
			if s, ok := child.(string); ok && (lower == "email" || strings.HasSuffix(lower, "_email")) {
				out[redactViewerString(key)] = maskViewerEmail(s)
				continue
			}
			if s, ok := child.(string); ok && (lower == "ip" || lower == "client_ip" || strings.HasSuffix(lower, "_ip")) {
				out[key] = maskViewerIP(s)
				continue
			}
			out[redactViewerString(key)] = redactViewerValue(child)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, child := range v {
			out[i] = redactViewerValue(child)
		}
		return out
	case string:
		return redactViewerString(v)
	default:
		return v
	}
}

func redactViewerString(s string) string {
	if strings.Contains(s, "@") {
		s = viewerURLUserinfo.ReplaceAllString(s, "$1")
		s = viewerEmailPattern.ReplaceAllStringFunc(s, maskViewerEmail)
	}
	return s
}

// maskViewerEmail 把邮箱打码为 ab***@domain；非邮箱字符串只保留前两个字符。
func maskViewerEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	local, domain, found := strings.Cut(email, "@")
	if !found {
		domain = ""
	}
	runes := []rune(local)
	keep := 2
	if len(runes) <= 2 {
		keep = 1
	}
	if len(runes) < keep {
		keep = len(runes)
	}
	masked := string(runes[:keep]) + "***"
	if found {
		masked += "@" + domain
	}
	return masked
}

// maskViewerIP 只保留 IP 的前两段：IPv4 → 203.0.*.*，IPv6 → 2001:db8:*。
func maskViewerIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	if strings.Contains(ip, ":") {
		parts := strings.Split(ip, ":")
		if len(parts) > 2 {
			parts = parts[:2]
		}
		return strings.Join(parts, ":") + ":*"
	}
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return "*"
	}
	return parts[0] + "." + parts[1] + ".*.*"
}

// ==================== 路由与 handler ====================

func (h *Handler) registerViewerAccessRoutes(api *gin.RouterGroup) {
	api.GET("/me", h.GetAdminMe)
	api.GET("/viewer-keys", h.ListAdminViewerKeys)
	api.POST("/viewer-keys", h.CreateAdminViewerKey)
	api.DELETE("/viewer-keys/:id", h.RevokeAdminViewerKey)
}

// GetAdminMe 返回当前登录身份。GET /api/admin/me
func (h *Handler) GetAdminMe(c *gin.Context) {
	if isViewer(c) {
		c.JSON(http.StatusOK, gin.H{"role": adminRoleViewer, "name": c.GetString(ctxAdminViewerName)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"role": adminRoleAdmin, "name": adminRoleAdmin})
}

type adminViewerKeyResponse struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	Revoked    bool       `json:"revoked"`
}

func toAdminViewerKeyResponse(k database.AdminViewerKey) adminViewerKeyResponse {
	resp := adminViewerKeyResponse{ID: k.ID, Name: k.Name, KeyPrefix: k.KeyPrefix, CreatedAt: k.CreatedAt}
	if k.LastUsedAt.Valid {
		t := k.LastUsedAt.Time
		resp.LastUsedAt = &t
	}
	if k.RevokedAt.Valid {
		t := k.RevokedAt.Time
		resp.RevokedAt = &t
		resp.Revoked = true
	}
	return resp
}

// ListAdminViewerKeys 列出只读密钥（不含明文）。GET /api/admin/viewer-keys
func (h *Handler) ListAdminViewerKeys(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	keys, err := h.db.ListAdminViewerKeys(ctx)
	if err != nil {
		writeInternalError(c, err)
		return
	}
	out := make([]adminViewerKeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, toAdminViewerKeyResponse(k))
	}
	c.JSON(http.StatusOK, gin.H{"keys": out})
}

// CreateAdminViewerKey 创建只读密钥，明文只在本次响应中返回。POST /api/admin/viewer-keys {"name":"..."}
func (h *Handler) CreateAdminViewerKey(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "请求体解析失败")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(c, http.StatusBadRequest, "名称不能为空")
		return
	}
	if len([]rune(name)) > viewerKeyNameMaxLen {
		writeError(c, http.StatusBadRequest, fmt.Sprintf("名称不能超过 %d 个字符", viewerKeyNameMaxLen))
		return
	}
	plaintext, err := generateViewerKey()
	if err != nil {
		writeInternalError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	prefix := viewerKeyDisplayPrefix(plaintext)
	id, err := h.db.CreateAdminViewerKey(ctx, name, prefix, hashViewerKey(plaintext))
	if err != nil {
		writeInternalError(c, err)
		return
	}
	security.SecurityAuditLog("ADMIN_VIEWER_KEY_CREATED", fmt.Sprintf("id=%d key=%s ip=%s", id, security.SanitizeLog(name), security.SanitizeLog(c.ClientIP())))
	c.JSON(http.StatusOK, gin.H{
		"id":         id,
		"name":       name,
		"key_prefix": prefix,
		"key":        plaintext,
		"created_at": time.Now().UTC(),
	})
}

// RevokeAdminViewerKey 吊销只读密钥并立即清除缓存。DELETE /api/admin/viewer-keys/:id
func (h *Handler) RevokeAdminViewerKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, http.StatusBadRequest, "无效的 ID")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	hash, err := h.db.RevokeAdminViewerKey(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(c, http.StatusNotFound, "只读密钥不存在")
			return
		}
		writeInternalError(c, err)
		return
	}
	h.viewerKeys.invalidate(hash)
	security.SecurityAuditLog("ADMIN_VIEWER_KEY_REVOKED", fmt.Sprintf("id=%d ip=%s", id, security.SanitizeLog(c.ClientIP())))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
