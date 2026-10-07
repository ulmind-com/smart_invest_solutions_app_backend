package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// ClientProductAccessHandler handles HTTP requests for per-client catalog visibility.
type ClientProductAccessHandler struct {
	service domain.ClientProductAccessService
}

// NewClientProductAccessHandler creates a new ClientProductAccessHandler.
func NewClientProductAccessHandler(service domain.ClientProductAccessService) *ClientProductAccessHandler {
	return &ClientProductAccessHandler{service: service}
}

// Get returns which catalog products one client can see.
// @Summary      Get a client's product visibility (Agency staff)
// @Description  Returns the client's catalog setting: mode `all` (the whole published catalog — also the answer for a client nobody has restricted) or `selected` with the product IDs they may see. A plain admin may only read a client of their own agency.
// @Tags         Products
// @Produce      json
// @Param        id  path  string  true  "Client user ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.ClientProductAccessDTO}  "Product visibility retrieved successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request — not a client account, or an invalid ID"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — agency staff only"
// @Security     BearerAuth
// @Router       /users/{id}/product-access [get]
func (h *ClientProductAccessHandler) Get(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	access, err := h.service.GetForClient(c.Request.Context(), claims.Role, claims.UserID.Hex(), c.Param("id"))
	if err != nil {
		response.Error(c, statusForAccessError(err), err.Error())
		return
	}

	response.Success(c, "Product visibility retrieved successfully", access)
}

// Set replaces which catalog products one client can see.
// @Summary      Set a client's product visibility (Agency staff)
// @Description  Replaces the client's catalog setting. `mode: "all"` restores the whole published catalog; `mode: "selected"` limits them to `product_ids`, and an empty list means they see no products. Every ID must exist, so a stale selection is rejected rather than silently dropped. A plain admin may only set this for a client of their own agency.
// @Tags         Products
// @Accept       json
// @Produce      json
// @Param        id       path  string                            true  "Client user ID (MongoDB ObjectID)"
// @Param        request  body  domain.SetClientProductAccessDTO  true  "Mode and, for 'selected', the product IDs"
// @Success      200  {object}  response.APIResponse{data=domain.ClientProductAccessDTO}  "Product visibility updated successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request — unknown product, not a client account, or an invalid ID"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — agency staff only"
// @Failure      422  {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /users/{id}/product-access [put]
func (h *ClientProductAccessHandler) Set(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var dto domain.SetClientProductAccessDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	access, err := h.service.SetForClient(c.Request.Context(), claims.Role, claims.UserID.Hex(), c.Param("id"), &dto)
	if err != nil {
		response.Error(c, statusForAccessError(err), err.Error())
		return
	}

	response.Success(c, "Product visibility updated successfully", access)
}

// statusForAccessError keeps "you may not do this" distinct from "that request was wrong", so the
// app can tell an admin whose session lacks the right from an admin who sent a stale selection.
func statusForAccessError(err error) int {
	if strings.Contains(err.Error(), "access denied") {
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}
