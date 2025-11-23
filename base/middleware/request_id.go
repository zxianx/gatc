package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	RequestIDKey = "X-Request-ID"
)

// RequestID 中间件，为每个请求生成唯一ID
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 先从header中获取request-id，如果没有则生成一个
		requestID := c.GetHeader(RequestIDKey)
		if requestID == "" {
			// 生成格式：时间戳+随机数
			requestID = fmt.Sprintf("req_%d_%d", 
				time.Now().UnixNano()/1e6, // 毫秒时间戳
				time.Now().Nanosecond()%1000) // 3位随机数
		}
		
		// 设置到context中
		c.Set(RequestIDKey, requestID)
		
		// 设置到response header中
		c.Header(RequestIDKey, requestID)
		
		c.Next()
	}
}

// GetRequestID 从gin.Context中获取requestID
func GetRequestID(c *gin.Context) string {
	if requestID, exists := c.Get(RequestIDKey); exists {
		return requestID.(string)
	}
	return ""
}