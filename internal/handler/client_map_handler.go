package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// ClientMapHandler handles HTTP requests for the client map.
type ClientMapHandler struct {
	clientMapService domain.ClientMapService
}

// NewClientMapHandler creates a new ClientMapHandler.
func NewClientMapHandler(clientMapService domain.ClientMapService) *ClientMapHandler {
	return &ClientMapHandler{clientMapService: clientMapService}
}

// GetClientMap returns the client map: who holds what, under which admin, and who is on the app.
// @Summary      Client map (Agency staff)
// @Description  One row per client: their policy and deposit counts, the admin whose agency they belong to, and whether they have ever signed in to the app. A super_admin sees every client and may narrow to one agency with `agency_id` (or to the clients belonging to none with `agency_id=unassigned`); a plain admin always sees only their own agency. The `summary` totals the whole filtered scope, so it does not move when `app` or `holdings` change.
// @Tags         Clients
// @Produce      json
// @Param        q          query     string  false  "Search name, email or phone"
// @Param        agency_id  query     string  false  "Agency ID to narrow to, or 'unassigned' (super_admin only — ignored for a plain admin)"
// @Param        app        query     string  false  "App presence filter"  Enums(on_app, not_on_app, no_access)
// @Param        holdings   query     string  false  "Holdings filter"      Enums(with, without)
// @Param        page       query     int     false  "Page number (default 1)"
// @Param        limit      query     int     false  "Rows per page (default 25, max 100)"
// @Success      200  {object}  response.APIResponse{data=domain.ClientMapPage}  "Client map retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      403  {object}  response.APIResponse  "Forbidden — agency staff only"
// @Failure      500  {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /client-map [get]
func (h *ClientMapHandler) GetClientMap(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "25"), 10, 64)

	query := domain.ClientMapQuery{
		AgencyID:  c.Query("agency_id"),
		Search:    c.Query("q"),
		AppStatus: c.Query("app"),
		Holdings:  c.Query("holdings"),
		Page:      page,
		Limit:     limit,
	}

	result, err := h.clientMapService.GetClientMap(c.Request.Context(), claims.Role, claims.UserID.Hex(), query)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	// Echoed back normalized rather than as requested: the service clamps paging, and a reader on a
	// page past the end has to be told which page they actually got.
	normalized := normalizeClientMapPaging(page, limit)

	response.Success(c, "Client map retrieved successfully", &domain.ClientMapPage{
		Items:      result.Items,
		Summary:    result.Summary,
		Total:      result.Total,
		Page:       normalized.page,
		Limit:      normalized.limit,
		TotalPages: totalPages(result.Total, normalized.limit),
	})
}

// clientMapPaging mirrors the service's own clamping so the response describes the page that was
// actually read.
type clientMapPaging struct {
	page  int64
	limit int64
}

func normalizeClientMapPaging(page, limit int64) clientMapPaging {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	return clientMapPaging{page: page, limit: limit}
}

// totalPages is at least 1, so an empty table reads as "page 1 of 1" instead of "page 1 of 0".
func totalPages(total, limit int64) int64 {
	if limit < 1 {
		return 1
	}
	pages := total / limit
	if total%limit != 0 {
		pages++
	}
	if pages < 1 {
		return 1
	}
	return pages
}
