package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// DocumentHandler handles HTTP requests for E-Vault document management.
type DocumentHandler struct {
	service domain.DocumentService
}

// NewDocumentHandler creates a new instance of DocumentHandler.
func NewDocumentHandler(service domain.DocumentService) *DocumentHandler {
	return &DocumentHandler{
		service: service,
	}
}

// UploadDocument handles uploading a new file (Aadhaar, PAN, Bank Passbook, Policy, etc.) to E-Vault.
// @Summary      Upload document to E-Vault
// @Description  Uploads a PDF or image file with auto-compression to Cloudinary and saves metadata to MongoDB E-Vault.
// @Tags         E-Vault Documents
// @Accept       multipart/form-data
// @Produce      json
// @Param        file      formData  file    true   "Document File (PDF, PNG, JPG, JPEG)"
// @Param        name      formData  string  true   "Document Name (e.g., Aadhaar Card)"
// @Param        category  formData  string  false  "Category (Aadhaar, PAN, Bank Passbook, Policy, Custom)"
// @Success      201       {object}  response.APIResponse{data=domain.Document}  "Document uploaded successfully"
// @Failure      400       {object}  response.APIResponse  "Bad request"
// @Failure      401       {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /documents [post]
func (h *DocumentHandler) UploadDocument(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	name := c.PostForm("name")
	if name == "" {
		response.Error(c, http.StatusBadRequest, "document name is required")
		return
	}

	category := c.PostForm("category")
	if category == "" {
		category = "General"
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "file is required")
		return
	}

	fileStream, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to open uploaded file")
		return
	}
	defer fileStream.Close()

	doc, err := h.service.UploadDocument(c.Request.Context(), userIDStr, name, category, fileStream, fileHeader.Filename)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Document uploaded successfully", doc)
}

// GetMyDocuments handles listing & searching all documents belonging to the authenticated user.
// @Summary      Get & Search E-Vault documents
// @Description  Retrieves list of all documents uploaded by the user with optional search query filter `q`.
// @Tags         E-Vault Documents
// @Accept       json
// @Produce      json
// @Param        q    query     string  false  "Search filter by name or category (e.g. aadhaar, pan, passbook)"
// @Success      200  {object}  response.APIResponse{data=domain.DocumentListResponse}  "Documents retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /documents [get]
func (h *DocumentHandler) GetMyDocuments(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	searchQuery := c.Query("q")

	respData, err := h.service.GetMyDocuments(c.Request.Context(), userIDStr, searchQuery)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Documents retrieved successfully", respData)
}

// GetByID handles retrieving a single document by ID.
// @Summary      Get document by ID
// @Description  Retrieves detailed info and secure link for a single document.
// @Tags         E-Vault Documents
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Document ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.Document}  "Document retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      404  {object}  response.APIResponse  "Document not found"
// @Security     BearerAuth
// @Router       /documents/{id} [get]
func (h *DocumentHandler) GetByID(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	doc, err := h.service.GetDocumentByID(c.Request.Context(), idStr, userIDStr)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Document retrieved successfully", doc)
}

// UpdateDocument handles updating a document's details or replacing its file (old Cloudinary file is purged).
// @Summary      Update / Replace document
// @Description  Updates document name/category or replaces file. Replacing a file purges the old Cloudinary asset automatically.
// @Tags         E-Vault Documents
// @Accept       multipart/form-data
// @Produce      json
// @Param        id        path      string  true   "Document ID"
// @Param        name      formData  string  false  "New Document Name"
// @Param        category  formData  string  false  "New Category"
// @Param        file      formData  file    false  "New Replacement File (PDF, PNG, JPG)"
// @Success      200       {object}  response.APIResponse{data=domain.Document}  "Document updated successfully"
// @Failure      400       {object}  response.APIResponse  "Bad request"
// @Failure      401       {object}  response.APIResponse  "Unauthorized"
// @Failure      404       {object}  response.APIResponse  "Document not found"
// @Security     BearerAuth
// @Router       /documents/{id} [put]
func (h *DocumentHandler) UpdateDocument(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	var dto domain.UpdateDocumentDTO

	name := c.PostForm("name")
	if name != "" {
		dto.Name = &name
	}

	category := c.PostForm("category")
	if category != "" {
		dto.Category = &category
	}

	var newFileStream io.Reader
	var filename string

	fileHeader, err := c.FormFile("file")
	if err == nil && fileHeader != nil {
		stream, errStream := fileHeader.Open()
		if errStream == nil {
			defer stream.Close()
			newFileStream = stream
			filename = fileHeader.Filename
		}
	}

	doc, err := h.service.UpdateDocument(c.Request.Context(), idStr, userIDStr, &dto, newFileStream, filename)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Document updated successfully", doc)
}

// DeleteDocument handles deleting a document from MongoDB AND purging its file from Cloudinary storage.
// @Summary      Delete document
// @Description  Deletes document record from MongoDB and purges file from Cloudinary storage.
// @Tags         E-Vault Documents
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Document ID"
// @Success      200  {object}  response.APIResponse  "Document deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /documents/{id} [delete]
func (h *DocumentHandler) DeleteDocument(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	if err := h.service.DeleteDocument(c.Request.Context(), idStr, userIDStr); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Document deleted successfully", nil)
}

// GetDocumentsByUserIDAdmin handles fetching client documents for Admin portal.
// @Summary      Get user's E-Vault documents (Admin Only)
// @Description  Retrieves all E-Vault documents for a target client user.
// @Tags         E-Vault Documents (Admin)
// @Accept       json
// @Produce      json
// @Param        userId  path      string  true   "Target Client User ID"
// @Param        q       query     string  false  "Search filter"
// @Success      200     {object}  response.APIResponse{data=domain.DocumentListResponse}  "Documents retrieved successfully"
// @Failure      401     {object}  response.APIResponse  "Unauthorized"
// @Failure      403     {object}  response.APIResponse  "Forbidden — Admin role required"
// @Security     BearerAuth
// @Router       /documents/user/{userId} [get]
func (h *DocumentHandler) GetDocumentsByUserIDAdmin(c *gin.Context) {
	targetUserIDStr := c.Param("userId")
	searchQuery := c.Query("q")

	respData, err := h.service.GetDocumentsByUserIDAdmin(c.Request.Context(), targetUserIDStr, searchQuery)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Documents retrieved successfully", respData)
}
