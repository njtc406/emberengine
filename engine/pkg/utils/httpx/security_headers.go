package httpx

import "github.com/gin-gonic/gin"

// SecurityHeadersConf 控制默认安全响应头行为。
type SecurityHeadersConf struct {
	// Enable 是否启用安全头（默认 true）
	Enable bool
	// HSTS 是否启用 Strict-Transport-Security（仅建议在 HTTPS 环境启用）
	HSTS bool
	// FrameOptions X-Frame-Options 值（默认 "DENY"）
	FrameOptions string
	// CSP Content-Security-Policy 值（默认 "default-src 'self'"）
	CSP string
	// ReferrerPolicy 值（默认 "no-referrer"）
	ReferrerPolicy string
}

// DefaultSecurityHeadersConf 返回默认安全头配置。
func DefaultSecurityHeadersConf() SecurityHeadersConf {
	return SecurityHeadersConf{
		Enable:         true,
		HSTS:           false, // 默认关闭，由用户在 HTTPS 环境下显式开启
		FrameOptions:   "DENY",
		CSP:            "default-src 'self'",
		ReferrerPolicy: "no-referrer",
	}
}

// SecurityHeaders 返回一个 Gin 中间件，为每个响应设置安全头。
// 业务代码可在 handler 中覆盖特定头。
func SecurityHeaders(conf SecurityHeadersConf) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !conf.Enable {
			c.Next()
			return
		}

		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")

		if conf.FrameOptions != "" {
			c.Header("X-Frame-Options", conf.FrameOptions)
		}
		if conf.ReferrerPolicy != "" {
			c.Header("Referrer-Policy", conf.ReferrerPolicy)
		}
		if conf.CSP != "" {
			c.Header("Content-Security-Policy", conf.CSP)
		}
		if conf.HSTS {
			c.Header("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}

		c.Next()
	}
}
