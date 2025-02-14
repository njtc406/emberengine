// Package auth
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2024/1/16 0016 10:30
// 最后更新:  yr  2024/1/16 0016 10:30
package auth

import (
	"encoding/base64"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func BasicAuth(accountMap map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := strings.SplitN(strings.TrimSpace(c.GetHeader("Authorization")), " ", 2)
		if len(auth) != 2 || auth[0] != "Basic" {
			c.Header("WWW-Authenticate", "Basic realm=\"Restricted Content\"")
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		payload, _ := base64.StdEncoding.DecodeString(auth[1])
		pair := strings.SplitN(string(payload), ":", 2)

		if len(pair) != 2 || accountMap[pair[0]] != pair[1] {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// 认证成功，继续执行下一个中间件或路由处理函数
		c.Next()
	}
}
