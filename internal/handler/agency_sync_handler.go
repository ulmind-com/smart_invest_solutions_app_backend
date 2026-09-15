package handler

import (
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
	"github.com/smart-invest-solutions/backend/pkg/utils"
)

// requireAgencyAdmin resolves the caller for every Agency Sync route. Agency Sync is deliberately
// admin-only — unlike every other admin-gated route in this API, a super_admin is NOT allowed
// (RequireRole's usual "super_admin can do anything admin can" bypass has to be overridden
// explicitly, since it can't be turned off in the shared middleware without affecting every other
// admin-only route). A super_admin has no agency of their own, so there is no inbox to show them.
func requireAgencyAdmin(c *gin.Context) (*utils.Claims, bool) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	if claims.Role == domain.RoleSuperAdmin {
		response.Error(c, http.StatusForbidden, "Agency Sync is only available to admin accounts")
		return nil, false
	}
	return claims, true
}

// AgencySyncHandler handles HTTP requests for agency PDF sync engine operations.
type AgencySyncHandler struct {
	agencySyncService domain.AgencySyncService
}

// NewAgencySyncHandler creates a new AgencySyncHandler.
func NewAgencySyncHandler(agencySyncService domain.AgencySyncService) *AgencySyncHandler {
	return &AgencySyncHandler{
		agencySyncService: agencySyncService,
	}
}

// ProcessLICDueList handles uploading and bulk-syncing Life Insurance policies from an LIC Premium Due List PDF.
// @Summary      Process LIC Premium Due List PDF (Admin only, not Super Admin)
// @Description  Uploads and parses an LIC Premium Due List PDF file, extracts policy numbers, assured names, DOC, FUP, Mode, and Premiums, calculates next due dates, updates existing policies in MongoDB, and returns unmapped policy records. This is a day-to-day operational task for regular agency admins — Super Admin accounts are deliberately excluded, unlike every other admin-only route in this API.
// @Tags         Agency Sync
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData  file  true  "LIC Premium Due List PDF File (.pdf)"
// @Success      200   {object}  response.APIResponse{data=domain.SyncResultDTO}  "LIC Premium Due List PDF processed successfully"
// @Failure      400   {object}  response.APIResponse  "Bad request — missing file or invalid PDF file format"
// @Failure      401   {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      403   {object}  response.APIResponse  "Forbidden — admin role required (super_admin excluded)"
// @Failure      500   {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /agency/sync/lic-due-list [post]
func (h *AgencySyncHandler) ProcessLICDueList(c *gin.Context) {
	claims, ok := requireAgencyAdmin(c)
	if !ok {
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "PDF file is required in 'file' form field")
		return
	}

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if ext != ".pdf" {
		response.Error(c, http.StatusBadRequest, "Invalid file format: only PDF files (.pdf) are supported")
		return
	}

	// Open and read file stream into memory
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "Failed to open uploaded PDF file: "+err.Error())
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to read uploaded PDF file bytes: "+err.Error())
		return
	}

	if len(fileBytes) == 0 {
		response.Error(c, http.StatusBadRequest, "Uploaded PDF file is empty")
		return
	}

	result, err := h.agencySyncService.ProcessLICDueList(c.Request.Context(), claims.Role, claims.UserID.Hex(), fileBytes)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "LIC Premium Due List PDF processed successfully", result)
}

// ListImportedPolicies handles browsing the agency's policy inbox — every row ever read from a due
// list, whether or not it has been attached to a client account yet.
// @Summary      List imported LIC policies (Admin only, not Super Admin)
// @Description  Returns the calling admin's policy inbox: every policy row imported from their LIC due lists, with live link status. Filter with status=unclaimed (no client account yet), status=linked (already attached to a client), or status=all. Search matches policy number or assured name.
// @Tags         Agency Sync
// @Accept       json
// @Produce      json
// @Param        status  query     string  false  "unclaimed | linked | all (default: all)"
// @Param        q       query     string  false  "Search by policy number or assured name"
// @Param        page    query     int     false  "Page number (default: 1)"
// @Param        limit   query     int     false  "Items per page (default: 20, max: 100)"
// @Success      200     {object}  response.PaginatedResponse{data=[]domain.ImportedPolicyView}  "Imported policies retrieved successfully"
// @Failure      401     {object}  response.APIResponse  "Unauthorized"
// @Failure      403     {object}  response.APIResponse  "Forbidden — admin role required (super_admin excluded)"
// @Security     BearerAuth
// @Router       /agency/imported-policies [get]
func (h *AgencySyncHandler) ListImportedPolicies(c *gin.Context) {
	claims, ok := requireAgencyAdmin(c)
	if !ok {
		return
	}

	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "20"), 10, 64)

	policies, total, err := h.agencySyncService.ListImportedPolicies(
		c.Request.Context(), claims.Role, claims.UserID.Hex(),
		c.Query("status"), c.Query("q"), page, limit,
	)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.SuccessWithPagination(c, "Imported policies retrieved successfully", policies, page, limit, total)
}

// LinkImportedPolicy handles attaching an unclaimed imported policy to a client account.
// @Summary      Link an imported LIC policy to a client (Admin only, not Super Admin)
// @Description  Creates the client's Life Insurance record from an imported due-list row. The policy number carries over, so every later due-list upload keeps this policy's premium and next due date current automatically. Sum assured (and optionally nominee/plan name) must be supplied because an LIC due list does not carry them.
// @Tags         Agency Sync
// @Accept       json
// @Produce      json
// @Param        id       path      string                        true  "Imported policy ID"
// @Param        request  body      domain.LinkImportedPolicyDTO  true  "Client account, insured family member, and sum assured"
// @Success      201      {object}  response.APIResponse{data=domain.LifeInsurance}  "Policy linked successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request — already linked, wrong client, or missing sum assured"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      403      {object}  response.APIResponse  "Forbidden — admin role required (super_admin excluded)"
// @Security     BearerAuth
// @Router       /agency/imported-policies/{id}/link [post]
func (h *AgencySyncHandler) LinkImportedPolicy(c *gin.Context) {
	claims, ok := requireAgencyAdmin(c)
	if !ok {
		return
	}

	var dto domain.LinkImportedPolicyDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	policy, err := h.agencySyncService.LinkImportedPolicy(
		c.Request.Context(), claims.Role, claims.UserID.Hex(), c.Param("id"), &dto,
	)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Policy linked to client successfully", policy)
}

// DeleteImportedPolicy handles removing a row from the agency's policy inbox.
// @Summary      Remove an imported LIC policy row (Admin only, not Super Admin)
// @Description  Deletes an imported due-list row from the agency's inbox — used when the wrong file was uploaded. A row already linked to a client's policy is refused; the client's actual policy is never touched by this endpoint.
// @Tags         Agency Sync
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Imported policy ID"
// @Success      200  {object}  response.APIResponse  "Imported policy removed successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request — row is linked to a client policy"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — admin role required (super_admin excluded)"
// @Security     BearerAuth
// @Router       /agency/imported-policies/{id} [delete]
func (h *AgencySyncHandler) DeleteImportedPolicy(c *gin.Context) {
	claims, ok := requireAgencyAdmin(c)
	if !ok {
		return
	}

	if err := h.agencySyncService.DeleteImportedPolicy(
		c.Request.Context(), claims.Role, claims.UserID.Hex(), c.Param("id"),
	); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Imported policy removed successfully", nil)
}
