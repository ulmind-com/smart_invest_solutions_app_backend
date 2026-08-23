package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type fixedDepositService struct {
	repo             domain.FixedDepositRepository
	userRepo         domain.UserRepository
	familyMemberRepo domain.FamilyMemberRepository
}

// NewFixedDepositService creates a new instance of FixedDepositService.
func NewFixedDepositService(repo domain.FixedDepositRepository, userRepo domain.UserRepository, familyMemberRepo domain.FamilyMemberRepository) domain.FixedDepositService {
	return &fixedDepositService{
		repo:             repo,
		userRepo:         userRepo,
		familyMemberRepo: familyMemberRepo,
	}
}

// resolveTargetUserID determines which client account a Fixed Deposit belongs to: a client
// always acts on their own account; admin/super_admin may target any client via dto.UserID.
func (s *fixedDepositService) resolveTargetUserID(requesterRole, requesterID, dtoUserID string) (bson.ObjectID, error) {
	targetIDStr := requesterID
	if (requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin) && dtoUserID != "" {
		targetIDStr = dtoUserID
	}
	return bson.ObjectIDFromHex(targetIDStr)
}

// checkOwnership enforces that a client requester may only touch their own Fixed Deposits;
// admin/super_admin bypass this check completely.
func (s *fixedDepositService) checkOwnership(requesterRole, requesterID string, ownerID bson.ObjectID) error {
	if requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin {
		return nil
	}
	requesterObjID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return fmt.Errorf("invalid requester ID format: %w", err)
	}
	if ownerID != requesterObjID {
		return fmt.Errorf("access denied: fixed deposit does not belong to you")
	}
	return nil
}

// CreateFD adds a new Fixed Deposit after verifying the target client exists, the family member
// (1st Holder) actually belongs to that client (prevents ID spoofing), and the dates are
// chronologically sound (OpeningDate before MaturityDate).
func (s *fixedDepositService) CreateFD(ctx context.Context, requesterRole, requesterID string, dto *domain.CreateFixedDepositDTO) (*domain.FixedDeposit, error) {
	userID, err := s.resolveTargetUserID(requesterRole, requesterID, dto.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	targetUser, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || targetUser == nil {
		return nil, fmt.Errorf("target user not found")
	}

	familyMemberID, err := bson.ObjectIDFromHex(dto.FamilyMemberID)
	if err != nil {
		return nil, fmt.Errorf("invalid family member ID format: %w", err)
	}

	familyMember, err := s.familyMemberRepo.FindByID(ctx, familyMemberID)
	if err != nil || familyMember == nil {
		return nil, fmt.Errorf("family member not found")
	}
	if familyMember.UserID != userID {
		return nil, fmt.Errorf("family member does not belong to the specified client")
	}

	if !dto.OpeningDate.Before(dto.MaturityDate) {
		return nil, fmt.Errorf("opening date must be before the maturity date")
	}

	fd := &domain.FixedDeposit{
		UserID:           userID,
		FamilyMemberID:   familyMemberID,
		FDNumber:         dto.FDNumber,
		FDName:           dto.FDName,
		CompanyName:      dto.CompanyName,
		PrincipalAmount:  dto.PrincipalAmount,
		MaturityAmount:   dto.MaturityAmount,
		Term:             dto.Term,
		OpeningDate:      dto.OpeningDate,
		MaturityDate:     dto.MaturityDate,
		NomineeName:      dto.NomineeName,
		SecondHolderName: dto.SecondHolderName,
		AccountType:      dto.AccountType,
		Address:          dto.Address,
		IsMapped:         dto.IsMapped,
	}

	return s.repo.Create(ctx, fd)
}

// GetFDByID retrieves a single Fixed Deposit, enforcing that a client requester owns it.
func (s *fixedDepositService) GetFDByID(ctx context.Context, requesterRole, requesterID, idStr string) (*domain.FixedDeposit, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid fixed deposit ID format: %w", err)
	}

	fd, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(requesterRole, requesterID, fd.UserID); err != nil {
		return nil, err
	}

	return fd, nil
}

// GetMyFDs retrieves all Fixed Deposits belonging to the authenticated client.
func (s *fixedDepositService) GetMyFDs(ctx context.Context, requesterID string) (*domain.FixedDepositListResponse, error) {
	userID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	fds, total, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &domain.FixedDepositListResponse{Total: total, Data: fds}, nil
}

// GetAllFDs returns the paginated Admin master list across every client, optionally filtered to
// unmapped/mapped Fixed Deposits.
func (s *fixedDepositService) GetAllFDs(ctx context.Context, page, limit int64, isMapped *bool) ([]*domain.FixedDepositWithCustomer, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	return s.repo.GetAll(ctx, page, limit, isMapped)
}

// UpdateFD modifies an existing Fixed Deposit, enforcing ownership for client requesters,
// re-verifying any reassigned family member belongs to the FD's owner, and re-validating
// OpeningDate/MaturityDate chronology.
//
// Security patch: IsMapped is an admin-tracking flag. If the requester is a client, whatever
// they sent in IsMapped is discarded (set back to nil) BEFORE hitting the repository, so the
// partial $set update in the repository never touches that field — the existing DB value is
// silently preserved. Only admin/super_admin may actually change it.
func (s *fixedDepositService) UpdateFD(ctx context.Context, requesterRole, requesterID, idStr string, dto *domain.UpdateFixedDepositDTO) (*domain.FixedDeposit, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid fixed deposit ID format: %w", err)
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(requesterRole, requesterID, existing.UserID); err != nil {
		return nil, err
	}

	if requesterRole != domain.RoleAdmin && requesterRole != domain.RoleSuperAdmin {
		dto.IsMapped = nil
	}

	if dto.FamilyMemberID != nil {
		familyMemberID, err := bson.ObjectIDFromHex(*dto.FamilyMemberID)
		if err != nil {
			return nil, fmt.Errorf("invalid family member ID format: %w", err)
		}
		familyMember, err := s.familyMemberRepo.FindByID(ctx, familyMemberID)
		if err != nil || familyMember == nil {
			return nil, fmt.Errorf("family member not found")
		}
		if familyMember.UserID != existing.UserID {
			return nil, fmt.Errorf("family member does not belong to the fixed deposit owner")
		}
	}

	openingDate := existing.OpeningDate
	if dto.OpeningDate != nil {
		openingDate = *dto.OpeningDate
	}
	maturityDate := existing.MaturityDate
	if dto.MaturityDate != nil {
		maturityDate = *dto.MaturityDate
	}
	if !openingDate.Before(maturityDate) {
		return nil, fmt.Errorf("opening date must be before the maturity date")
	}

	return s.repo.Update(ctx, id, dto)
}

// DeleteFD removes a Fixed Deposit, enforcing ownership for client requesters.
func (s *fixedDepositService) DeleteFD(ctx context.Context, requesterRole, requesterID, idStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid fixed deposit ID format: %w", err)
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.checkOwnership(requesterRole, requesterID, existing.UserID); err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		log.Warn().Err(err).Str("fd_id", idStr).Msg("failed to delete fixed deposit")
		return err
	}

	return nil
}

// DeleteAllByUserID removes all Fixed Deposits associated with a user ID string — wired into the
// account-deletion cascade alongside documents/family members/general & life insurance.
func (s *fixedDepositService) DeleteAllByUserID(ctx context.Context, userIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}
	return s.repo.DeleteAllByUserID(ctx, userID)
}
