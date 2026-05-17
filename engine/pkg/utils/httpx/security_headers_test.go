package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestSecurityHeaders_Default(t *testing.T) {
	r := gin.New()
	r.Use(SecurityHeaders(DefaultSecurityHeadersConf()))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "1; mode=block", w.Header().Get("X-XSS-Protection"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	assert.Equal(t, "default-src 'self'", w.Header().Get("Content-Security-Policy"))
	assert.Empty(t, w.Header().Get("Strict-Transport-Security")) // HSTS off by default
}

func TestSecurityHeaders_HSTS(t *testing.T) {
	conf := DefaultSecurityHeadersConf()
	conf.HSTS = true

	r := gin.New()
	r.Use(SecurityHeaders(conf))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	assert.Contains(t, w.Header().Get("Strict-Transport-Security"), "max-age=")
}

func TestSecurityHeaders_Disabled(t *testing.T) {
	conf := SecurityHeadersConf{Enable: false}

	r := gin.New()
	r.Use(SecurityHeaders(conf))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	assert.Empty(t, w.Header().Get("X-Content-Type-Options"))
	assert.Empty(t, w.Header().Get("X-Frame-Options"))
}

func TestSecurityHeaders_CustomValues(t *testing.T) {
	conf := SecurityHeadersConf{
		Enable:         true,
		FrameOptions:   "SAMEORIGIN",
		CSP:            "default-src 'none'",
		ReferrerPolicy: "strict-origin",
	}

	r := gin.New()
	r.Use(SecurityHeaders(conf))
	r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, "SAMEORIGIN", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "default-src 'none'", w.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "strict-origin", w.Header().Get("Referrer-Policy"))
}

func TestSecurityHeaders_BusinessOverride(t *testing.T) {
	r := gin.New()
	r.Use(SecurityHeaders(DefaultSecurityHeadersConf()))
	r.GET("/test", func(c *gin.Context) {
		c.Header("X-Frame-Options", "SAMEORIGIN") // 业务覆盖
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	// 业务覆盖后应为 SAMEORIGIN
	assert.Equal(t, "SAMEORIGIN", w.Header().Get("X-Frame-Options"))
}
