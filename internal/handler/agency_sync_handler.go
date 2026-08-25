package handler

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

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
// @Summary      Process LIC Premium Due List PDF (Admin only)
// @Description  Uploads and parses an LIC Premium Due List PDF file, extracts policy numbers, assured names, DOC, FUP, Mode, and Premiums, calculates next due dates, updates existing policies in MongoDB, and returns unmapped policy records.
// @Tags         Agency Sync
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData  file  true  "LIC Premium Due List PDF File (.pdf)"
// @Success      200   {object}  response.APIResponse{data=domain.SyncResultDTO}  "LIC Premium Due List PDF processed successfully"
// @Failure      400   {object}  response.APIResponse  "Bad request — missing file or invalid PDF file format"
// @Failure      401   {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      403   {object}  response.APIResponse  "Forbidden — admin role required"
// @Failure      500   {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /agency/sync/lic-due-list [post]
func (h *AgencySyncHandler) ProcessLICDueList(c *gin.Context) {
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

	result, err := h.agencySyncService.ProcessLICDueList(c.Request.Context(), fileBytes)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "LIC Premium Due List PDF processed successfully", result)
}
