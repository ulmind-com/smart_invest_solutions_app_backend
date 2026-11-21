package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockCronService struct {
	mock.Mock
}

func (m *mockCronService) RunDailyMaintenance(ctx context.Context) (*domain.CronExecutionResult, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.CronExecutionResult), args.Error(1)
}

func setupCronTestRouter(svc domain.CronService, secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	cfg := &config.Config{CronSecret: secret}
	h := handler.NewCronHandler(svc, cfg)

	group := r.Group("/api/v1/cron")
	{
		group.GET("/health", h.Health)
		group.POST("/run", h.RunCron)
		group.GET("/run", h.RunCron)
	}
	return r
}

func TestCronHandler_Health(t *testing.T) {
	svc := new(mockCronService)
	r := setupCronTestRouter(svc, "super-secret-key")

	// 1. Unauthenticated health check
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cron/health", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "ok", resp["status"])
	assert.Equal(t, true, resp["cron_ready"])
	assert.Equal(t, false, resp["authenticated"])

	// 2. Authenticated health check via query param
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/cron/health?secret=super-secret-key", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, true, resp["authenticated"])
}

func TestCronHandler_RunCron_Unauthorized(t *testing.T) {
	svc := new(mockCronService)
	r := setupCronTestRouter(svc, "super-secret-key")

	// Request with missing secret
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cron/run", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Request with wrong secret
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/cron/run?secret=wrong-key", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCronHandler_RunCron_Authorized_Variations(t *testing.T) {
	mockResult := &domain.CronExecutionResult{
		Status:                "success",
		ExecutedAt:            time.Now().UTC(),
		DurationMs:            42,
		AdminExpiryAlertsSent: 1,
		AdminAccountsChecked:  2,
		ActiveAdminsExpired:   0,
		RenewalsSummary:       map[string]interface{}{"overdue_count": 3},
		Message:               "Daily maintenance completed successfully",
	}

	testCases := []struct {
		name       string
		method     string
		url        string
		headers    map[string]string
	}{
		{
			name:   "Via X-Cron-Secret header (POST)",
			method: http.MethodPost,
			url:    "/api/v1/cron/run",
			headers: map[string]string{
				"X-Cron-Secret": "super-secret-key",
			},
		},
		{
			name:   "Via Authorization Bearer header (POST)",
			method: http.MethodPost,
			url:    "/api/v1/cron/run",
			headers: map[string]string{
				"Authorization": "Bearer super-secret-key",
			},
		},
		{
			name:    "Via query parameter ?secret= (GET for UptimeRobot)",
			method:  http.MethodGet,
			url:     "/api/v1/cron/run?secret=super-secret-key",
			headers: nil,
		},
		{
			name:    "Via query parameter ?key= (GET)",
			method:  http.MethodGet,
			url:     "/api/v1/cron/run?key=super-secret-key",
			headers: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := new(mockCronService)
			svc.On("RunDailyMaintenance", mock.Anything).Return(mockResult, nil).Once()
			r := setupCronTestRouter(svc, "super-secret-key")

			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.url, nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}

			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)

			var res map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &res)
			assert.NoError(t, err)
			assert.Equal(t, true, res["success"])
			svc.AssertExpectations(t)
		})
	}
}
