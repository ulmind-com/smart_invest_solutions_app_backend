package domain

import (
	"context"
	"io"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Document represents an E-Vault document record associated with a user.
type Document struct {
	ID          bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID      bson.ObjectID `bson:"user_id" json:"user_id"`
	Name        string        `bson:"name" json:"name" binding:"required"`
	Category    string        `bson:"category" json:"category"` // e.g. Aadhaar Card, PAN Card, Bank Passbook, Policy, Custom
	DocumentURL string        `bson:"document_url" json:"document_url"`
	PublicID    string        `bson:"public_id" json:"public_id"`
	FileType    string        `bson:"file_type,omitempty" json:"file_type,omitempty"`
	FileSize    int64         `bson:"file_size,omitempty" json:"file_size,omitempty"` // in bytes
	CreatedAt   time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time     `bson:"updated_at" json:"updated_at"`
}

// CreateDocumentDTO represents the payload for creating a new document record.
type CreateDocumentDTO struct {
	Name        string `json:"name" form:"name" binding:"required"`
	Category    string `json:"category" form:"category"`
	DocumentURL string `json:"document_url" form:"document_url"`
	PublicID    string `json:"public_id" form:"public_id"`
	FileType    string `json:"file_type" form:"file_type"`
	FileSize    int64  `json:"file_size" form:"file_size"`
}

// UpdateDocumentDTO represents the payload for updating an existing document record.
type UpdateDocumentDTO struct {
	Name        *string `json:"name,omitempty" form:"name"`
	Category    *string `json:"category,omitempty" form:"category"`
	DocumentURL *string `json:"document_url,omitempty" form:"document_url"`
	PublicID    *string `json:"public_id,omitempty" form:"public_id"`
	FileType    *string `json:"file_type,omitempty" form:"file_type"`
	FileSize    *int64  `json:"file_size,omitempty" form:"file_size"`
}

// DocumentListResponse represents the list response for documents with total count.
type DocumentListResponse struct {
	Total int64       `json:"total"`
	Data  []*Document `json:"data"`
}

// DocumentRepository defines database operations for documents.
type DocumentRepository interface {
	Create(ctx context.Context, doc *Document) (*Document, error)
	FindAllByUserID(ctx context.Context, userID bson.ObjectID, searchQuery string) ([]*Document, int64, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*Document, error)
	Update(ctx context.Context, id, userID bson.ObjectID, update *UpdateDocumentDTO) (*Document, error)
	Delete(ctx context.Context, id, userID bson.ObjectID) error
	DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error
}

// DocumentService defines business logic operations for documents.
type DocumentService interface {
	UploadDocument(ctx context.Context, userIDStr, name, category string, file io.Reader, filename string) (*Document, error)
	GetMyDocuments(ctx context.Context, userIDStr, searchQuery string) (*DocumentListResponse, error)
	GetDocumentByID(ctx context.Context, idStr, userIDStr string) (*Document, error)
	UpdateDocument(ctx context.Context, idStr, userIDStr string, dto *UpdateDocumentDTO, newFile io.Reader, filename string) (*Document, error)
	DeleteDocument(ctx context.Context, idStr, userIDStr string) error
	GetDocumentsByUserIDAdmin(ctx context.Context, targetUserIDStr, searchQuery string) (*DocumentListResponse, error)
	DeleteAllByUserID(ctx context.Context, userIDStr string) error
}
