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

// requireAgencyAdmin resolves the caller for every Agency Sync route.
//
// Agency Sync always acts on one agency's book, so each route needs to know *whose*. A plain admin
// has exactly one and the service uses it whatever they send. A super_admin has none of their own and
// names the agency they are working on behalf of — passed as `agency_id` (a query parameter on the
// reads, a form field on the uploads, since those are multipart). The service validates it and
// refuses an unknown one, so nothing here has to trust the value.
func requireAgencyAdmin(c *gin.Context) (*utils.Claims, bool) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return claims, true
}

// actingAgencyID is the agency a super_admin is working on behalf of, read from wherever this request
// can carry it. Ignored by the service for a plain admin, so reading it unconditionally is safe.
func actingAgencyID(c *gin.Context) string {
	if fromQuery := strings.TrimSpace(c.Query("agency_id")); fromQuery != "" {
		return fromQuery
	}
	return strings.TrimSpace(c.PostForm("agency_id"))
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
// @Param        agency_id  query     string  false  "Super admin only: the Agency ID to act on behalf of — required for a super_admin, who has no agency book of their own. Ignored for a plain admin. The upload routes also accept it as a form field."
// @Success      200   {object}  response.APIResponse{data=domain.SyncResultDTO}  "LIC Premium Due List PDF processed successfully"
// @Failure      400   {object}  response.APIResponse  "Bad request — missing file or invalid PDF file format"
// @Failure      401   {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      403   {object}  response.APIResponse  "Forbidden — agency staff only"
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

	// A due list is a few hundred KB; cap it so an oversized upload can't be read wholesale into memory.
	const maxDueListBytes = 20 << 20
	if fileHeader.Size > maxDueListBytes {
		response.Error(c, http.StatusBadRequest, "The PDF is larger than 20 MB — upload the due list exactly as downloaded from the LIC portal")
		return
	}

	// Open and read file stream into memory
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "Failed to open uploaded PDF file: "+err.Error())
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(io.LimitReader(file, maxDueListBytes+1))
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to read uploaded PDF file bytes: "+err.Error())
		return
	}

	if len(fileBytes) == 0 {
		response.Error(c, http.StatusBadRequest, "Uploaded PDF file is empty")
		return
	}

	result, err := h.agencySyncService.ProcessLICDueList(c.Request.Context(), claims.Role, claims.UserID.Hex(), actingAgencyID(c), fileBytes)
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
// @Param        agency_id  query     string  false  "Super admin only: the Agency ID to act on behalf of — required for a super_admin, who has no agency book of their own. Ignored for a plain admin. The upload routes also accept it as a form field."
// @Success      200     {object}  response.PaginatedResponse{data=[]domain.ImportedPolicyView}  "Imported policies retrieved successfully"
// @Failure      401     {object}  response.APIResponse  "Unauthorized"
// @Failure      403     {object}  response.APIResponse  "Forbidden — agency staff only"
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
		c.Request.Context(), claims.Role, claims.UserID.Hex(), actingAgencyID(c),
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
// @Param        agency_id  query     string  false  "Super admin only: the Agency ID to act on behalf of — required for a super_admin, who has no agency book of their own. Ignored for a plain admin. The upload routes also accept it as a form field."
// @Success      201      {object}  response.APIResponse{data=domain.LifeInsurance}  "Policy linked successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request — already linked, wrong client, or missing sum assured"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      403      {object}  response.APIResponse  "Forbidden — agency staff only"
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
		c.Request.Context(), claims.Role, claims.UserID.Hex(), actingAgencyID(c), c.Param("id"), &dto,
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
// @Param        agency_id  query     string  false  "Super admin only: the Agency ID to act on behalf of — required for a super_admin, who has no agency book of their own. Ignored for a plain admin. The upload routes also accept it as a form field."
// @Success      200  {object}  response.APIResponse  "Imported policy removed successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request — row is linked to a client policy"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — agency staff only"
// @Security     BearerAuth
// @Router       /agency/imported-policies/{id} [delete]
func (h *AgencySyncHandler) DeleteImportedPolicy(c *gin.Context) {
	claims, ok := requireAgencyAdmin(c)
	if !ok {
		return
	}

	if err := h.agencySyncService.DeleteImportedPolicy(
		c.Request.Context(), claims.Role, claims.UserID.Hex(), actingAgencyID(c), c.Param("id"),
	); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Imported policy removed successfully", nil)
}

// ProcessPostalReport imports a Post Office report PDF.
// @Summary      Import a Post Office report (Admin only, not Super Admin)
// @Description  Reads every deposit account out of a post office report: accounts a client of this agency already holds are refreshed, and the rest wait in the deposit inbox until an admin attaches them to a client.
// @Tags         Agency Sync
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData  file  true  "Post Office report (PDF)"
// @Param        agency_id  query     string  false  "Super admin only: the Agency ID to act on behalf of — required for a super_admin, who has no agency book of their own. Ignored for a plain admin. The upload routes also accept it as a form field."
// @Success      200   {object}  response.APIResponse{data=domain.DepositSyncResultDTO}  "Report processed successfully"
// @Failure      400   {object}  response.APIResponse  "Bad request — missing file or invalid PDF"
// @Failure      401   {object}  response.APIResponse  "Unauthorized"
// @Failure      403   {object}  response.APIResponse  "Forbidden — agency admin only"
// @Security     BearerAuth
// @Router       /agency/sync/postal-report [post]
func (h *AgencySyncHandler) ProcessPostalReport(c *gin.Context) {
	claims, ok := requireAgencyAdmin(c)
	if !ok {
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "PDF file is required in 'file' form field")
		return
	}

	if ext := strings.ToLower(filepath.Ext(fileHeader.Filename)); ext != ".pdf" {
		response.Error(c, http.StatusBadRequest, "Invalid file format: only PDF files (.pdf) are supported")
		return
	}

	// A report is a few hundred KB; cap it so an oversized upload can't be read wholesale into memory.
	const maxReportBytes = 20 << 20
	if fileHeader.Size > maxReportBytes {
		response.Error(c, http.StatusBadRequest, "The PDF is larger than 20 MB — upload the report exactly as downloaded")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "Failed to open uploaded PDF file: "+err.Error())
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(io.LimitReader(file, maxReportBytes+1))
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to read uploaded PDF file bytes: "+err.Error())
		return
	}
	if len(fileBytes) == 0 {
		response.Error(c, http.StatusBadRequest, "Uploaded PDF file is empty")
		return
	}

	result, err := h.agencySyncService.ProcessPostalReport(c.Request.Context(), claims.Role, claims.UserID.Hex(), actingAgencyID(c), fileHeader.Filename, fileBytes)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Post Office report processed successfully", result)
}

// ListImportedDeposits browses the agency's deposit inbox.
// @Summary      List imported deposits (Admin only, not Super Admin)
// @Description  Every account read from the agency's post office reports, with live link status.
// @Tags         Agency Sync
// @Produce      json
// @Param        status  query  string  false  "unclaimed | linked | all (default all)"
// @Param        q       query  string  false  "Search account number or holder name"
// @Param        page    query  int     false  "Page number (default 1)"
// @Param        limit   query  int     false  "Rows per page (default 20, max 100)"
// @Param        agency_id  query     string  false  "Super admin only: the Agency ID to act on behalf of — required for a super_admin, who has no agency book of their own. Ignored for a plain admin. The upload routes also accept it as a form field."
// @Success      200  {object}  response.APIResponse{data=[]domain.ImportedDepositView}  "Imported deposits retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — agency admin only"
// @Security     BearerAuth
// @Router       /agency/imported-deposits [get]
func (h *AgencySyncHandler) ListImportedDeposits(c *gin.Context) {
	claims, ok := requireAgencyAdmin(c)
	if !ok {
		return
	}

	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "20"), 10, 64)

	deposits, total, err := h.agencySyncService.ListImportedDeposits(
		c.Request.Context(), claims.Role, claims.UserID.Hex(), actingAgencyID(c),
		c.Query("status"), c.Query("q"), page, limit,
	)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.SuccessWithPagination(c, "Imported deposits retrieved successfully", deposits, page, limit, total)
}

// LinkImportedDeposit attaches an imported deposit to a client account.
// @Summary      Link an imported deposit to a client (Admin only, not Super Admin)
// @Description  Creates the client's Fixed Deposit record from the report row plus the holder the admin picks.
// @Tags         Agency Sync
// @Accept       json
// @Produce      json
// @Param        id       path  string                          true  "Imported deposit ID"
// @Param        request  body  domain.LinkImportedDepositDTO   true  "Client and holder to attach it to"
// @Param        agency_id  query     string  false  "Super admin only: the Agency ID to act on behalf of — required for a super_admin, who has no agency book of their own. Ignored for a plain admin. The upload routes also accept it as a form field."
// @Success      201  {object}  response.APIResponse{data=domain.FixedDeposit}  "Deposit linked successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — agency admin only"
// @Security     BearerAuth
// @Router       /agency/imported-deposits/{id}/link [post]
func (h *AgencySyncHandler) LinkImportedDeposit(c *gin.Context) {
	claims, ok := requireAgencyAdmin(c)
	if !ok {
		return
	}

	var dto domain.LinkImportedDepositDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	deposit, err := h.agencySyncService.LinkImportedDeposit(c.Request.Context(), claims.Role, claims.UserID.Hex(), actingAgencyID(c), c.Param("id"), &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Deposit linked to client successfully", deposit)
}

// DeleteImportedDeposit removes an unlinked row from the deposit inbox.
// @Summary      Remove an imported deposit (Admin only, not Super Admin)
// @Description  Deletes an inbox row that is not linked to a client deposit — e.g. the wrong report was uploaded.
// @Tags         Agency Sync
// @Produce      json
// @Param        id  path  string  true  "Imported deposit ID"
// @Param        agency_id  query     string  false  "Super admin only: the Agency ID to act on behalf of — required for a super_admin, who has no agency book of their own. Ignored for a plain admin. The upload routes also accept it as a form field."
// @Success      200  {object}  response.APIResponse  "Imported deposit removed"
// @Failure      400  {object}  response.APIResponse  "Bad request — still linked to a client"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — agency admin only"
// @Security     BearerAuth
// @Router       /agency/imported-deposits/{id} [delete]
func (h *AgencySyncHandler) DeleteImportedDeposit(c *gin.Context) {
	claims, ok := requireAgencyAdmin(c)
	if !ok {
		return
	}

	if err := h.agencySyncService.DeleteImportedDeposit(c.Request.Context(), claims.Role, claims.UserID.Hex(), actingAgencyID(c), c.Param("id")); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Imported deposit removed", nil)
}
