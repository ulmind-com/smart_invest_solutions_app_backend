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
// super_admin bypasses this check completely, and a plain admin may only touch a Fixed Deposit
// whose owning client belongs to their own agency (same resolveCallerAgencyID/
// canAccessAgencyScopedRecord pattern used for the admin master list) — a cross-agency FD reports
// "not found" rather than "forbidden".
func (s *fixedDepositService) checkOwnership(ctx context.Context, requesterRole, requesterID string, ownerID bson.ObjectID) error {
	if requesterRole == domain.RoleSuperAdmin {
		return nil
	}
	if requesterRole == domain.RoleAdmin {
		owner, err := s.userRepo.FindByID(ctx, ownerID)
		if err != nil || owner == nil {
			return fmt.Errorf("fixed deposit not found")
		}
		agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, owner.AgencyID) {
			return fmt.Errorf("fixed deposit not found")
		}
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

	// A plain admin may only create a Fixed Deposit on behalf of a client under their own agency.
	if requesterRole == domain.RoleAdmin {
		agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, targetUser.AgencyID) {
			return nil, fmt.Errorf("target user not found")
		}
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

	if err := s.checkOwnership(ctx, requesterRole, requesterID, fd.UserID); err != nil {
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

// GetFDsByUserIDAdmin lets admin/super_admin view a specific client's full, unpaginated Fixed
// Deposit list. A plain admin may only look up a client belonging to their own agency (same
// resolveCallerAgencyID/canAccessAgencyScopedRecord pattern used everywhere else).
func (s *fixedDepositService) GetFDsByUserIDAdmin(ctx context.Context, requesterRole, requesterID, targetUserIDStr string) (*domain.FixedDepositListResponse, error) {
	targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid target user ID format: %w", err)
	}

	if requesterRole == domain.RoleAdmin {
		targetUser, err := s.userRepo.FindByID(ctx, targetUserID)
		if err != nil || targetUser == nil {
			return nil, fmt.Errorf("user not found")
		}
		agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, targetUser.AgencyID) {
			return nil, fmt.Errorf("user not found")
		}
	}

	fds, total, err := s.repo.GetByUserID(ctx, targetUserID)
	if err != nil {
		return nil, err
	}

	return &domain.FixedDepositListResponse{Total: total, Data: fds}, nil
}

// GetAllFDs returns the paginated Admin master list across every client, optionally filtered to
// unmapped/mapped Fixed Deposits. A plain admin only ever sees FDs belonging to their own agency's
// clients; super_admin sees every agency (see resolveCallerAgencyID / canAccessAgencyScopedRecord
// in user_service.go).
func (s *fixedDepositService) GetAllFDs(ctx context.Context, requesterRole, requesterID string, page, limit int64, isMapped *bool) ([]*domain.FixedDepositWithCustomer, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	if requesterRole == domain.RoleAdmin && agencyFilter == "" {
		// Fail closed, exactly like the other agency-scoped listings: an admin whose own agency
		// can't be resolved must never fall through to the platform-wide (super_admin) view.
		return []*domain.FixedDepositWithCustomer{}, 0, nil
	}

	return s.repo.GetAll(ctx, page, limit, isMapped, agencyFilter)
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

	if err := s.checkOwnership(ctx, requesterRole, requesterID, existing.UserID); err != nil {
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

	if err := s.checkOwnership(ctx, requesterRole, requesterID, existing.UserID); err != nil {
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
