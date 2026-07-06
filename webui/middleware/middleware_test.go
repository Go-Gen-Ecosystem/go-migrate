package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func TestRequestID(t *testing.T) {
	router := setupTestRouter()
	router.Use(RequestID())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, req)

	requestID := w.Header().Get(constants.HeaderRequestID)
	assert.NotEmpty(t, requestID)
}

func TestRequestID_WithExistingID(t *testing.T) {
	router := setupTestRouter()
	router.Use(RequestID())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(constants.HeaderRequestID, "existing-id")
	router.ServeHTTP(w, req)

	requestID := w.Header().Get(constants.HeaderRequestID)
	assert.Equal(t, "existing-id", requestID)
}

func TestLogger(t *testing.T) {
	logger := logging.NewSimpleLogger()
	router := setupTestRouter()
	router.Use(Logger(logger))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRecovery(t *testing.T) {
	logger := logging.NewSimpleLogger()
	router := setupTestRouter()
	router.Use(Recovery(logger))
	router.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), constants.ErrCodeInternalError)
}

func TestSecurityHeaders(t *testing.T) {
	router := setupTestRouter()
	router.Use(SecurityHeaders())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "1; mode=block", w.Header().Get("X-XSS-Protection"))
}

func TestCORS(t *testing.T) {
	router := setupTestRouter()
	router.Use(CORS([]string{"http://localhost:3000"}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	t.Run("Allowed origin", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		router.ServeHTTP(w, req)

		assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("Disallowed origin", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", "http://evil.com")
		router.ServeHTTP(w, req)

		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("OPTIONS request", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

func TestMaxRequestSize(t *testing.T) {
	router := setupTestRouter()
	router.Use(MaxRequestSize(100)) // 100 bytes max
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	t.Run("Within limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		req.ContentLength = 50
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Exceeds limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		req.ContentLength = 200
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	})
}

func TestRateLimit(t *testing.T) {
	router := setupTestRouter()
	router.Use(RateLimit(2, 1*time.Second)) // 2 requests per second
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// First two requests should succeed
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Third request should be rate limited
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestAuth(t *testing.T) {
	router := setupTestRouter()
	router.Use(Auth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	t.Run("Without token", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("With token", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set(constants.HeaderAuthorization, "Bearer test-token")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestRBAC(t *testing.T) {
	router := setupTestRouter()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "test-user")
		c.Next()
	})
	router.Use(RBAC())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTimeout(t *testing.T) {
	router := setupTestRouter()
	router.Use(Timeout(100 * time.Millisecond))
	router.GET("/test", func(c *gin.Context) {
		time.Sleep(50 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
