package middleware

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
)

// Recovery returns a middleware that recovers from panics
func Recovery(logger logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Error(c.Request.Context(), "Panic recovered",
					fmt.Errorf("%v", err))

				c.JSON(http.StatusInternalServerError, gin.H{
					"error": constants.MsgInternalError,
					"code":  constants.ErrCodeInternalError,
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}

// RequestID returns a middleware that adds a unique request ID
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(constants.HeaderRequestID)
		if requestID == "" {
			requestID = uuid.New().String()
		}
		c.Set("request_id", requestID)
		c.Header(constants.HeaderRequestID, requestID)
		c.Next()
	}
}

// Logger returns a middleware that logs HTTP requests
func Logger(logger logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		clientIP := c.ClientIP()
		requestID := c.GetString("request_id")

		fields := []interface{}{
			"status", status,
			"method", method,
			"path", path,
			"query", query,
			"ip", clientIP,
			"latency", latency,
			"request_id", requestID,
		}

		if len(c.Errors) > 0 {
			logger.Error(c.Request.Context(), "Request completed with errors",
				c.Errors.Last())
			return
		}

		if status >= 500 {
			logger.Error(c.Request.Context(), "Request failed", fmt.Errorf("status %d", status))
		} else if status >= 400 {
			logger.Warn(c.Request.Context(), "Request warning", fmt.Errorf("status %d", status))
		} else {
			logger.Info(c.Request.Context(), "Request completed", fields...)
		}
	}
}

// SecurityHeaders returns a middleware that adds security headers
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Next()
	}
}

// CORS returns a middleware that handles CORS
func CORS(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			if allowedOrigin == "*" || allowedOrigin == origin {
				allowed = true
				break
			}
		}

		if allowed {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// RateLimit returns a middleware that implements rate limiting
func RateLimit(requestsPerWindow int, window time.Duration) gin.HandlerFunc {
	type client struct {
		requests int
		reset    time.Time
	}

	var (
		mu      sync.RWMutex
		clients = make(map[string]*client)
	)

	// Cleanup goroutine
	go func() {
		ticker := time.NewTicker(window)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for ip, c := range clients {
				if now.After(c.reset) {
					delete(clients, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()

		mu.Lock()
		cl, exists := clients[ip]
		if !exists {
			clients[ip] = &client{
				requests: 1,
				reset:    time.Now().Add(window),
			}
			mu.Unlock()
			c.Next()
			return
		}

		now := time.Now()
		if now.After(cl.reset) {
			cl.requests = 1
			cl.reset = now.Add(window)
			mu.Unlock()
			c.Next()
			return
		}

		if cl.requests >= requestsPerWindow {
			mu.Unlock()
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"code":  "RATE_LIMIT_EXCEEDED",
			})
			c.Abort()
			return
		}

		cl.requests++
		mu.Unlock()
		c.Next()
	}
}

// MaxRequestSize returns a middleware that limits request body size
func MaxRequestSize(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxSize {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "Request body too large",
				"code":  "REQUEST_TOO_LARGE",
			})
			c.Abort()
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		c.Next()
	}
}

// Gzip returns a middleware that compresses responses
func Gzip() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.Contains(c.Request.Header.Get("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}

		// Skip compression for WebSocket upgrades
		if c.Request.Header.Get("Upgrade") == "websocket" {
			c.Next()
			return
		}

		gz := gzip.NewWriter(c.Writer)
		defer gz.Close()

		c.Header("Content-Encoding", "gzip")
		c.Header("Vary", "Accept-Encoding")

		c.Writer = &gzipWriter{Writer: gz, ResponseWriter: c.Writer}
		c.Next()
	}
}

// gzipWriter wraps gin.ResponseWriter with gzip compression
type gzipWriter struct {
	gin.ResponseWriter
	Writer io.Writer
}

func (g *gzipWriter) Write(data []byte) (int, error) {
	return g.Writer.Write(data)
}

func (g *gzipWriter) WriteString(s string) (int, error) {
	return g.Writer.Write([]byte(s))
}

// Auth returns a middleware that handles authentication
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader(constants.HeaderAuthorization)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": constants.MsgUnauthorized,
				"code":  constants.ErrCodeUnauthorized,
			})
			c.Abort()
			return
		}

		// Remove "Bearer " prefix if present
		token = strings.TrimPrefix(token, "Bearer ")

		// TODO: Validate JWT token and extract user info
		// For now, just check if token exists
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": constants.MsgUnauthorized,
				"code":  constants.ErrCodeUnauthorized,
			})
			c.Abort()
			return
		}

		// Set user context
		// TODO: Extract from validated token
		c.Set("user_id", "user-123")
		c.Set("user_email", "user@example.com")

		c.Next()
	}
}

// RBAC returns a middleware that handles role-based access control
func RBAC() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error": constants.MsgForbidden,
				"code":  constants.ErrCodeForbidden,
			})
			c.Abort()
			return
		}

		// TODO: Check user permissions based on the requested resource
		// For now, allow all authenticated requests
		c.Next()
	}
}

// RequirePermission returns a middleware that checks for specific permissions
func RequirePermission(permission constants.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error": constants.MsgForbidden,
				"code":  constants.ErrCodeForbidden,
			})
			c.Abort()
			return
		}

		// TODO: Check if user has the required permission
		// For now, allow all authenticated requests
		hasPermission := true // Placeholder

		if !hasPermission {
			c.JSON(http.StatusForbidden, gin.H{
				"error": fmt.Sprintf("Permission required: %s", permission),
				"code":  constants.ErrCodeForbidden,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Timeout returns a middleware that adds request timeout
func Timeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
