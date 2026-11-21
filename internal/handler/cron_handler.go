package handler

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// CronHandler manages background maintenance runs triggered by external schedulers (UptimeRobot, cron-job.org).
type CronHandler struct {
	service domain.CronService
	cfg     *config.Config
	running atomic.Bool
}

// NewCronHandler creates a new instance of CronHandler.
func NewCronHandler(service domain.CronService, cfg *config.Config) *CronHandler {
	return &CronHandler{
		service: service,
		cfg:     cfg,
	}
}

// isAuthorized validates the request against the configured CRON_SECRET.
// Supports:
// 1. Header: X-Cron-Secret: <secret>
// 2. Header: Authorization: Bearer <secret>
// 3. Query Param: ?secret=<secret> or ?key=<secret>
func (h *CronHandler) isAuthorized(c *gin.Context) bool {
	secret := c.GetHeader("X-Cron-Secret")
	if secret == "" {
		auth := c.GetHeader("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			secret = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if secret == "" {
		secret = c.Query("secret")
	}
	if secret == "" {
		secret = c.Query("key")
	}

	expected := h.cfg.CronSecret
	if expected == "" {
		expected = "smartwealth-cron-secret-2026"
	}

	return secret != "" && secret == expected
}

// RunCron executes platform maintenance tasks.
// @Summary      Run scheduled background maintenance (Cron)
// @Description  Triggers admin expiry checks, renewal aggregation, and automated alert emails. Protected by CRON_SECRET via header or query parameter. Supports GET and POST for easy UptimeRobot / cron-job.org integration.
// @Tags         Cron
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=domain.CronExecutionResult}  "Cron maintenance executed successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized — invalid or missing cron secret"
// @Router       /cron/run [post]
func (h *CronHandler) RunCron(c *gin.Context) {
	if !h.isAuthorized(c) {
		response.Error(c, http.StatusUnauthorized, "unauthorized: invalid or missing cron secret")
		return
	}

	// Concurrency lock: prevent overlapping runs if duplicate pings arrive
	if !h.running.CompareAndSwap(false, true) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  "in_progress",
			"message": "A scheduled maintenance run is already executing; request skipped to prevent duplicate work.",
		})
		return
	}
	defer h.running.Store(false)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()

	result, err := h.service.RunDailyMaintenance(ctx)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Cron maintenance executed successfully", result)
}

// Health provides a fast, lightweight ping to confirm the cron subsystem is online without executing heavy tasks.
// @Summary      Check cron service readiness
// @Description  Returns 200 OK with server time and ready status. Open to uptime checkers.
// @Tags         Cron
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /cron/health [get]
func (h *CronHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":        "ok",
		"cron_ready":    true,
		"server_time":   time.Now().UTC().Format(time.RFC3339),
		"authenticated": h.isAuthorized(c),
	})
}
