package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Support ticket category enum values.
const (
	TicketCategoryConsultation = "Consultation"
	TicketCategoryClaimSupport = "ClaimSupport"
	TicketCategoryHelpDesk     = "HelpDesk"
	TicketCategoryAppSupport   = "AppSupport"
)

// Support ticket status enum values.
const (
	TicketStatusOpen       = "Open"
	TicketStatusInProgress = "In_Progress"
	TicketStatusResolved   = "Resolved"
	TicketStatusClosed     = "Closed"
)

// IsValidTicketCategory reports whether s is one of the recognized ticket categories.
func IsValidTicketCategory(s string) bool {
	switch s {
	case TicketCategoryConsultation, TicketCategoryClaimSupport, TicketCategoryHelpDesk, TicketCategoryAppSupport:
		return true
	}
	return false
}

// IsValidTicketStatus reports whether s is one of the recognized ticket statuses.
func IsValidTicketStatus(s string) bool {
	switch s {
	case TicketStatusOpen, TicketStatusInProgress, TicketStatusResolved, TicketStatusClosed:
		return true
	}
	return false
}

// SupportTicket represents a unified support request (consultation, claim support, help desk, or
// app support) opened by a client (UserID).
type SupportTicket struct {
	ID           bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID       bson.ObjectID `bson:"user_id" json:"user_id"`
	TicketNumber string        `bson:"ticket_number" json:"ticket_number"` // Auto-generated, e.g. "TKT-10023"
	Category     string        `bson:"category" json:"category"`
	Subject      string        `bson:"subject" json:"subject"`
	Description  string        `bson:"description" json:"description"`
	Status       string        `bson:"status" json:"status"`
	AdminNotes   string        `bson:"admin_notes,omitempty" json:"admin_notes,omitempty"` // Internal notes for the agency — admin/super_admin only
	CreatedAt    time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time     `bson:"updated_at" json:"updated_at"`
}

// SupportTicketWithCustomer represents a support ticket enriched with the requesting client's
// name and contact number — used for the Admin master list view.
type SupportTicketWithCustomer struct {
	ID           bson.ObjectID `bson:"_id" json:"id"`
	UserID       bson.ObjectID `bson:"user_id" json:"user_id"`
	CustomerName string        `bson:"customer_name" json:"customer_name"`
	ContactNo    string        `bson:"contact_no" json:"contact_no"`
	TicketNumber string        `bson:"ticket_number" json:"ticket_number"`
	Category     string        `bson:"category" json:"category"`
	Subject      string        `bson:"subject" json:"subject"`
	Description  string        `bson:"description" json:"description"`
	Status       string        `bson:"status" json:"status"`
	AdminNotes   string        `bson:"admin_notes,omitempty" json:"admin_notes,omitempty"`
	CreatedAt    time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time     `bson:"updated_at" json:"updated_at"`
}

// CreateSupportTicketDTO represents the payload for opening a new support ticket. UserID is
// optional and only honored for admin/super_admin requesters — a client always gets the ticket
// created under their own JWT account regardless of what's sent here.
type CreateSupportTicketDTO struct {
	UserID      string `json:"user_id,omitempty" example:"64f1a2b3c4d5e6f7a8b9c0d1"`
	Category    string `json:"category" binding:"required,oneof=Consultation ClaimSupport HelpDesk AppSupport"`
	Subject     string `json:"subject" binding:"required"`
	Description string `json:"description" binding:"required"`
}

// UpdateSupportTicketDTO represents the payload for partially updating an existing ticket — only
// supplied (non-nil) fields are modified. Status and AdminNotes are intentionally still bindable
// from JSON here (a client MAY send them), but the service layer strips/restricts them before
// persisting unless the requester is admin/super_admin — see supportTicketService.UpdateTicket.
type UpdateSupportTicketDTO struct {
	Subject     *string `json:"subject,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty" binding:"omitempty,oneof=Open In_Progress Resolved Closed"`
	AdminNotes  *string `json:"admin_notes,omitempty"`
}

// SupportTicketListResponse represents a single client's list of support tickets.
type SupportTicketListResponse struct {
	Total int64            `json:"total"`
	Data  []*SupportTicket `json:"data"`
}

// SupportTicketRepository defines database operations for support tickets.
type SupportTicketRepository interface {
	Create(ctx context.Context, ticket *SupportTicket) (*SupportTicket, error)
	GetByID(ctx context.Context, id bson.ObjectID) (*SupportTicket, error)
	GetByUserID(ctx context.Context, userID bson.ObjectID, status, category string) ([]*SupportTicket, int64, error)
	GetAll(ctx context.Context, page, limit int64, status, category string) ([]*SupportTicketWithCustomer, int64, error)
	Update(ctx context.Context, id bson.ObjectID, dto *UpdateSupportTicketDTO) (*SupportTicket, error)
	Delete(ctx context.Context, id bson.ObjectID) error
	DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error
}

// SupportTicketService defines business logic operations for support tickets.
type SupportTicketService interface {
	CreateTicket(ctx context.Context, requesterRole, requesterID string, dto *CreateSupportTicketDTO) (*SupportTicket, error)
	GetTicketByID(ctx context.Context, requesterRole, requesterID, idStr string) (*SupportTicket, error)
	GetMyTickets(ctx context.Context, requesterID, status, category string) (*SupportTicketListResponse, error)
	GetAllTickets(ctx context.Context, page, limit int64, status, category string) ([]*SupportTicketWithCustomer, int64, error)
	UpdateTicket(ctx context.Context, requesterRole, requesterID, idStr string, dto *UpdateSupportTicketDTO) (*SupportTicket, error)
	DeleteTicket(ctx context.Context, requesterRole, requesterID, idStr string) error
	DeleteAllByUserID(ctx context.Context, userIDStr string) error
}
