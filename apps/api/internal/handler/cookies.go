package handler

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"
)

// 会话 Cookie 名称。
const (
	// SessionCookie 保存 JWT，HttpOnly —— 前端脚本读不到，XSS 无法直接窃取。
	SessionCookie = "thesis_session"
	// CSRFCookie 保存 CSRF 令牌，必须可被 JS 读取（双提交方案的另一半）。
	CSRFCookie = "thesis_csrf"
	// CSRFHeader 是前端回传 CSRF 令牌时使用的请求头。
	CSRFHeader = "X-CSRF-Token"
)

// cookieConfig 描述当前环境的 Cookie 安全属性。
type cookieConfig struct {
	secure   bool // HTTPS 下必须为 true
	sameSite http.SameSite
}

// newCookieConfig 根据运行环境选择属性。
//
// 生产环境（HTTPS）：Secure + SameSite=Lax。
// 开发环境（http://localhost）：不能加 Secure，否则浏览器不会保存。
// SameSite 统一用 Lax：跨站 POST 不会带上 Cookie，配合双提交 Token 形成双层防护。
func newCookieConfig(isProduction bool) cookieConfig {
	return cookieConfig{
		secure:   isProduction,
		sameSite: http.SameSiteLaxMode,
	}
}

// setSessionCookie 写入会话 Cookie。
func setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration, cfg cookieConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.secure,
		SameSite: cfg.sameSite,
		MaxAge:   int(ttl.Seconds()),
	})
}

// setCSRFCookie 写入 CSRF Cookie。必须 HttpOnly=false —— 前端要读出来放进请求头。
func setCSRFCookie(w http.ResponseWriter, value string, ttl time.Duration, cfg cookieConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: false,
		Secure:   cfg.secure,
		SameSite: cfg.sameSite,
		MaxAge:   int(ttl.Seconds()),
	})
}

// clearAuthCookies 登出时清除两个 Cookie（MaxAge<0 立即失效）。
func clearAuthCookies(w http.ResponseWriter, cfg cookieConfig) {
	for _, name := range []string{SessionCookie, CSRFCookie} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: name == SessionCookie,
			Secure:   cfg.secure,
			SameSite: cfg.sameSite,
			MaxAge:   -1,
		})
	}
}

// newCSRFToken 生成一个高熵随机令牌（32 字节 → 43 字符 base64url）。
func newCSRFToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
