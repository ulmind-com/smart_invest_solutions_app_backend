package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// maxTicketNumberGenerationAttempts bounds how many times Create retries generating a fresh
// ticket number after a collision against the collection's unique index.
const maxTicketNumberGenerationAttempts = 5

type supportTicketService struct {
	repo     domain.SupportTicketRepository
	userRepo domain.UserRepository
}

// NewSupportTicketService creates a new instance of SupportTicketService.
func NewSupportTicketService(repo domain.SupportTicketRepository, userRepo domain.UserRepository) domain.SupportTicketService {
	return &supportTicketService{repo: repo, userRepo: userRepo}
}

// resolveTargetUserID determines which client account a ticket belongs to: a client always acts
// on their own account; admin/super_admin may target any client via dto.UserID.
func (s *supportTicketService) resolveTargetUserID(requesterRole, requesterID, dtoUserID string) (bson.ObjectID, error) {
	targetIDStr := requesterID
	if (requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin) && dtoUserID != "" {
		targetIDStr = dtoUserID
	}
	return bson.ObjectIDFromHex(targetIDStr)
}

// checkOwnership enforces that a client requester may only touch their own tickets;
// admin/super_admin bypass this check completely.
func (s *supportTicketService) checkOwnership(requesterRole, requesterID string, ownerID bson.ObjectID) error {
	if requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin {
		return nil
	}
	requesterObjID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return fmt.Errorf("invalid requester ID format: %w", err)
	}
	if ownerID != requesterObjID {
		return fmt.Errorf("access denied: ticket does not belong to you")
	}
	return nil
}

// CreateTicket opens a new support ticket after verifying the target client exists and the
// category is valid, auto-generating a unique ticket number (retrying on collision against the
// collection's unique index).
func (s *supportTicketService) CreateTicket(ctx context.Context, requesterRole, requesterID string, dto *domain.CreateSupportTicketDTO) (*domain.SupportTicket, error) {
	if !domain.IsValidTicketCategory(dto.Category) {
		return nil, fmt.Errorf("invalid category: %s", dto.Category)
	}

	userID, err := s.resolveTargetUserID(requesterRole, requesterID, dto.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	targetUser, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || targetUser == nil {
		return nil, fmt.Errorf("target user not found")
	}

	var created *domain.SupportTicket
	for attempt := 0; attempt < maxTicketNumberGenerationAttempts; attempt++ {
		ticketNumber, genErr := utils.GenerateTicketNumber()
		if genErr != nil {
			return nil, fmt.Errorf("failed to generate ticket number: %w", genErr)
		}

		ticket := &domain.SupportTicket{
			UserID:       userID,
			TicketNumber: ticketNumber,
			Category:     dto.Category,
			Subject:      dto.Subject,
			Description:  dto.Description,
			Status:       domain.TicketStatusOpen,
		}

		created, err = s.repo.Create(ctx, ticket)
		if err == nil {
			return created, nil
		}
		if err.Error() != "ticket number already exists" {
			return nil, err
		}
	}

	return nil, fmt.Errorf("failed to generate a unique ticket number, please retry")
}

// GetTicketByID retrieves a single ticket, enforcing that a client requester owns it.
func (s *supportTicketService) GetTicketByID(ctx context.Context, requesterRole, requesterID, idStr string) (*domain.SupportTicket, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid ticket ID format: %w", err)
	}

	ticket, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(requesterRole, requesterID, ticket.UserID); err != nil {
		return nil, err
	}

	return ticket, nil
}

// GetMyTickets retrieves all tickets belonging to the authenticated client, optionally filtered
// by status and/or category.
func (s *supportTicketService) GetMyTickets(ctx context.Context, requesterID, status, category string) (*domain.SupportTicketListResponse, error) {
	userID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}
	if status != "" && !domain.IsValidTicketStatus(status) {
		return nil, fmt.Errorf("invalid status: %s", status)
	}
	if category != "" && !domain.IsValidTicketCategory(category) {
		return nil, fmt.Errorf("invalid category: %s", category)
	}

	tickets, total, err := s.repo.GetByUserID(ctx, userID, status, category)
	if err != nil {
		return nil, err
	}

	return &domain.SupportTicketListResponse{Total: total, Data: tickets}, nil
}

// GetAllTickets returns the paginated Admin master list across every client, optionally filtered
// by status and/or category.
func (s *supportTicketService) GetAllTickets(ctx context.Context, page, limit int64, status, category string) ([]*domain.SupportTicketWithCustomer, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	if status != "" && !domain.IsValidTicketStatus(status) {
		return nil, 0, fmt.Errorf("invalid status: %s", status)
	}
	if category != "" && !domain.IsValidTicketCategory(category) {
		return nil, 0, fmt.Errorf("invalid category: %s", category)
	}

	return s.repo.GetAll(ctx, page, limit, status, category)
}

// UpdateTicket modifies an existing ticket, enforcing ownership for client requesters.
//
// RBAC / field-stripping: AdminNotes and Status are workflow-management fields reserved for the
// agency. If the requester is a client, AdminNotes is always stripped, and Status is stripped
// UNLESS it is being set to "Closed" (a client closing their own resolved query is allowed; any
// other status transition — e.g. reopening, marking Resolved/In_Progress — is silently discarded
// so the repository's partial $set update never touches it, preserving the existing DB value).
// Only admin/super_admin may set AdminNotes or transition Status freely.
func (s *supportTicketService) UpdateTicket(ctx context.Context, requesterRole, requesterID, idStr string, dto *domain.UpdateSupportTicketDTO) (*domain.SupportTicket, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid ticket ID format: %w", err)
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(requesterRole, requesterID, existing.UserID); err != nil {
		return nil, err
	}

	isAgency := requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin
	if !isAgency {
		dto.AdminNotes = nil
		if dto.Status != nil && *dto.Status != domain.TicketStatusClosed {
			dto.Status = nil
		}
	}

	if dto.Status != nil && !domain.IsValidTicketStatus(*dto.Status) {
		return nil, fmt.Errorf("invalid status: %s", *dto.Status)
	}

	return s.repo.Update(ctx, id, dto)
}

// DeleteTicket removes a ticket, enforcing ownership for client requesters. Route-level RBAC
// additionally restricts DELETE to super_admin only.
func (s *supportTicketService) DeleteTicket(ctx context.Context, requesterRole, requesterID, idStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid ticket ID format: %w", err)
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.checkOwnership(requesterRole, requesterID, existing.UserID); err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		log.Warn().Err(err).Str("ticket_id", idStr).Msg("failed to delete support ticket")
		return err
	}

	return nil
}

// DeleteAllByUserID removes all support tickets associated with a user ID string — wired into the
// account-deletion cascade alongside documents/family members/insurance policies/fixed deposits.
func (s *supportTicketService) DeleteAllByUserID(ctx context.Context, userIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}
	return s.repo.DeleteAllByUserID(ctx, userID)
}
