package handler

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// ProductHandler handles HTTP requests for the Product Catalog module.
type ProductHandler struct {
	service domain.ProductService
}

// NewProductHandler creates a new instance of ProductHandler.
func NewProductHandler(service domain.ProductService) *ProductHandler {
	return &ProductHandler{service: service}
}

// parseKeyBenefits normalizes the "key_benefits" form field(s) into a clean []string — it accepts
// either repeated "key_benefits" fields or a single comma-separated value.
func parseKeyBenefits(raw []string) []string {
	var values []string
	if len(raw) == 1 && strings.Contains(raw[0], ",") {
		values = strings.Split(raw[0], ",")
	} else {
		values = raw
	}

	benefits := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			benefits = append(benefits, v)
		}
	}
	return benefits
}

// CreateProduct handles adding a new catalog product (Admin/Super Admin only).
// @Summary      Add Product (Super Admin only)
// @Description  Creates a new catalog product with an optional brochure file upload. Super Admin only — a plain admin can browse the catalog but cannot create products.
// @Tags         Product Catalog
// @Accept       multipart/form-data
// @Produce      json
// @Param        name          formData  string  true   "Product Name"
// @Param        category      formData  string  true   "Category (Life, Health, General, FD, MutualFund)"
// @Param        description   formData  string  false  "Product Description"
// @Param        key_benefits  formData  string  false  "Key Benefits — comma-separated or repeated field"
// @Param        is_active     formData  bool    false  "Published status (default: true)"
// @Param        brochure      formData  file    false  "Brochure File (PDF, PNG, JPG)"
// @Success      201  {object}  response.APIResponse{data=domain.Product}  "Product created successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super_admin role required"
// @Security     BearerAuth
// @Router       /products [post]
func (h *ProductHandler) CreateProduct(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	name := c.PostForm("name")
	if name == "" {
		response.Error(c, http.StatusBadRequest, "product name is required")
		return
	}

	dto := domain.CreateProductDTO{
		Name:        name,
		Category:    c.PostForm("category"),
		Description: c.PostForm("description"),
		KeyBenefits: parseKeyBenefits(c.PostFormArray("key_benefits")),
	}

	if raw, exists := c.GetPostForm("is_active"); exists {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			dto.IsActive = &parsed
		} else {
			response.ValidationError(c, "is_active must be true or false")
			return
		}
	}

	var fileStream io.Reader
	var filename string
	if fileHeader, err := c.FormFile("brochure"); err == nil && fileHeader != nil {
		stream, errStream := fileHeader.Open()
		if errStream == nil {
			defer stream.Close()
			fileStream = stream
			filename = fileHeader.Filename
		}
	}

	product, err := h.service.CreateProduct(c.Request.Context(), claims.Role, &dto, fileStream, filename)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Product created successfully", product)
}

// GetProducts handles browsing the product catalog — paginated, with an optional category filter.
// @Summary      Get Products
// @Description  Retrieves a paginated list of catalog products. Clients/advisors only see published (is_active=true) products; admin/super_admin may pass is_active to filter, or omit it to see every product including inactive ones.
// @Tags         Product Catalog
// @Accept       json
// @Produce      json
// @Param        page       query     int     false  "Page number (default: 1)"
// @Param        limit      query     int     false  "Items per page (default: 10, max: 100)"
// @Param        category   query     string  false  "Filter by category (Life, Health, General, FD, MutualFund)"
// @Param        is_active  query     bool    false  "Filter by published status — Admin only"
// @Success      200  {object}  response.APIResponse{data=domain.ProductListResponse}  "Products retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /products [get]
func (h *ProductHandler) GetProducts(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "10"), 10, 64)
	category := c.Query("category")

	var isActive *bool
	if raw := c.Query("is_active"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			response.ValidationError(c, "is_active must be true or false")
			return
		}
		isActive = &parsed
	}

	respData, err := h.service.GetAllProducts(c.Request.Context(), claims.Role, page, limit, category, isActive)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.SuccessWithPagination(c, "Products retrieved successfully", respData.Data, page, limit, respData.Total)
}

// GetByID handles retrieving a single product by ID.
// @Summary      Get Product by ID
// @Description  Retrieves detailed info for a single product. Clients/advisors can only view published products; admin/super_admin can view any.
// @Tags         Product Catalog
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Product ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.Product}  "Product retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      404  {object}  response.APIResponse  "Product not found"
// @Security     BearerAuth
// @Router       /products/{id} [get]
func (h *ProductHandler) GetByID(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	product, err := h.service.GetProductByID(c.Request.Context(), claims.Role, idStr)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Product retrieved successfully", product)
}

// UpdateProduct handles updating an existing product, optionally replacing its brochure
// (Admin/Super Admin only; old brochure is purged from Cloudinary automatically).
// @Summary      Update Product (Super Admin only)
// @Description  Updates product fields and/or replaces the brochure file. Super Admin only — a plain admin can browse the catalog but cannot modify products.
// @Tags         Product Catalog
// @Accept       multipart/form-data
// @Produce      json
// @Param        id            path      string  true   "Product ID"
// @Param        name          formData  string  false  "New Product Name"
// @Param        category      formData  string  false  "New Category"
// @Param        description   formData  string  false  "New Description"
// @Param        key_benefits  formData  string  false  "New Key Benefits — comma-separated or repeated field"
// @Param        is_active     formData  bool    false  "New Published status"
// @Param        brochure      formData  file    false  "New Replacement Brochure File"
// @Success      200  {object}  response.APIResponse{data=domain.Product}  "Product updated successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super_admin role required"
// @Failure      404  {object}  response.APIResponse  "Product not found"
// @Security     BearerAuth
// @Router       /products/{id} [put]
func (h *ProductHandler) UpdateProduct(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	var dto domain.UpdateProductDTO

	if name := c.PostForm("name"); name != "" {
		dto.Name = &name
	}
	if category := c.PostForm("category"); category != "" {
		dto.Category = &category
	}
	if description, exists := c.GetPostForm("description"); exists {
		dto.Description = &description
	}
	if benefits := c.PostFormArray("key_benefits"); len(benefits) > 0 {
		parsed := parseKeyBenefits(benefits)
		dto.KeyBenefits = &parsed
	}
	if raw, exists := c.GetPostForm("is_active"); exists {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			response.ValidationError(c, "is_active must be true or false")
			return
		}
		dto.IsActive = &parsed
	}

	var newFileStream io.Reader
	var filename string
	if fileHeader, err := c.FormFile("brochure"); err == nil && fileHeader != nil {
		stream, errStream := fileHeader.Open()
		if errStream == nil {
			defer stream.Close()
			newFileStream = stream
			filename = fileHeader.Filename
		}
	}

	product, err := h.service.UpdateProduct(c.Request.Context(), claims.Role, idStr, &dto, newFileStream, filename)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Product updated successfully", product)
}

// DeleteProduct handles deleting a product and cascade-purging its brochure from Cloudinary
// (Admin/Super Admin only).
// @Summary      Delete Product (Super Admin only)
// @Description  Permanently removes a product and its brochure asset from Cloudinary. Super Admin only.
// @Tags         Product Catalog
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Product ID"
// @Success      200  {object}  response.APIResponse  "Product deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super_admin role required"
// @Security     BearerAuth
// @Router       /products/{id} [delete]
func (h *ProductHandler) DeleteProduct(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	if err := h.service.DeleteProduct(c.Request.Context(), claims.Role, idStr); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Product deleted successfully", nil)
}
