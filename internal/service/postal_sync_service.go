package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// defaultDepositInstitution is who a post office report's accounts are held with.
const defaultDepositInstitution = "India Post"

// ProcessPostalReport imports a Post Office report.
//
// It works exactly like the LIC due list sync, for the same reason: the report knows the account
// and the money, but not which app user it belongs to. So every row is stored in the agency's
// deposit inbox, accounts a client of this agency already holds are refreshed in place, and the
// rest wait in the inbox until an admin attaches them to an account.
func (s *agencySyncService) ProcessPostalReport(ctx context.Context, requesterRole, requesterID, agencyID, reportName string, fileBytes []byte) (*domain.DepositSyncResultDTO, error) {
	if len(fileBytes) == 0 {
		return nil, fmt.Errorf("uploaded file is empty")
	}

	agencyID, err := s.resolveSyncAgency(ctx, requesterRole, requesterID, agencyID)
	if err != nil {
		return nil, err
	}

	rawText, err := extractTextFromPDF(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to extract text from PDF: %w", err)
	}
	if strings.TrimSpace(rawText) == "" {
		return nil, fmt.Errorf("unable to read text from PDF file or file is empty")
	}

	parsed := parsePostalReport(rawText)

	result := &domain.DepositSyncResultDTO{
		ReportName:               strings.TrimSpace(reportName),
		TotalAccountsFoundInPDF:  len(parsed.Records),
		DuplicateAccountNumbers:  parsed.DuplicateAccounts,
		UnreadableAccountNumbers: parsed.UnparsedAccounts,
		FailedDeposits:           []domain.FailedSyncDeposit{},
		UnmappedDeposits:         []domain.UnmappedDeposit{},
	}

	if len(parsed.Records) == 0 {
		// Nothing readable: report the running inbox size so the screen still says something useful.
		if unclaimed, _, err := s.importedDepositRepo.CountByStatus(ctx, agencyID); err == nil {
			result.UnclaimedTotal = int(unclaimed)
		}
		return result, nil
	}

	// Step 1: every row goes into the agency's deposit inbox (idempotent on agency + account).
	inbox := make([]*domain.ImportedDeposit, 0, len(parsed.Records))
	for _, rec := range parsed.Records {
		inbox = append(inbox, &domain.ImportedDeposit{
			AgencyID:        agencyID,
			AccountNo:       rec.AccountNo,
			HolderName:      rec.HolderName,
			JointHolderName: rec.JointHolderName,
			Scheme:          rec.Scheme,
			SchemeCode:      rec.SchemeCode,
			TermMonths:      postalTermMonths(rec),
			DepositAmount:   rec.DepositAmount,
			MaturityAmount:  rec.MaturityAmount,
			MonthlyIncome:   rec.MonthlyIncome,
			IssueDate:       rec.IssueDate,
			MaturityDate:    rec.MaturityDate,
			Remarks:         rec.Remarks,
			ReportName:      result.ReportName,
		})
	}

	newlyImported, err := s.importedDepositRepo.BulkUpsertFromSync(ctx, inbox)
	if err != nil {
		return nil, fmt.Errorf("failed to store imported deposits: %w", err)
	}
	result.NewlyImported = int(newlyImported)

	// Step 2: refresh the deposits that already belong to a client of this agency.
	accountNos := make([]string, 0, len(parsed.Records))
	for _, rec := range parsed.Records {
		accountNos = append(accountNos, rec.AccountNo)
	}

	existing, err := s.fixedDepositRepo.GetExistingFDNumbers(ctx, accountNos, agencyID)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing deposits from database: %w", err)
	}

	updates := make([]domain.PostalDepositUpdate, 0, len(parsed.Records))
	for _, rec := range parsed.Records {
		if existing[rec.AccountNo] {
			updates = append(updates, domain.PostalDepositUpdate{
				FDNumber:        rec.AccountNo,
				PrincipalAmount: rec.DepositAmount,
				MaturityAmount:  maturityValueFor(rec),
				MonthlyIncome:   rec.MonthlyIncome,
				MaturityDate:    rec.MaturityDate,
				TermMonths:      postalTermMonths(rec),
			})
			continue
		}
		result.UnmappedDeposits = append(result.UnmappedDeposits, domain.UnmappedDeposit{
			AccountNo:       rec.AccountNo,
			HolderName:      rec.HolderName,
			JointHolderName: rec.JointHolderName,
			Scheme:          rec.Scheme,
			DepositAmount:   rec.DepositAmount,
			MaturityAmount:  rec.MaturityAmount,
			MonthlyIncome:   rec.MonthlyIncome,
			IssueDate:       rec.IssueDate,
			MaturityDate:    rec.MaturityDate,
		})
	}

	if len(updates) > 0 {
		updatedCount, failed, err := s.fixedDepositRepo.BulkUpdateFromPostalSync(ctx, updates, agencyID)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh existing deposits: %w", err)
		}
		result.SuccessfullyUpdatedInDB = int(updatedCount)
		if failed != nil {
			result.FailedDeposits = failed
		}
		result.FailedToUpdateInDB = len(result.FailedDeposits)
		// Matched but neither written nor failed means the report carried nothing new for it.
		if current := len(updates) - result.SuccessfullyUpdatedInDB - result.FailedToUpdateInDB; current > 0 {
			result.AlreadyCurrent = current
		}
	}

	if unclaimed, _, err := s.importedDepositRepo.CountByStatus(ctx, agencyID); err == nil {
		result.UnclaimedTotal = int(unclaimed)
	}

	return result, nil
}

// ListImportedDeposits returns the calling admin's deposit inbox.
func (s *agencySyncService) ListImportedDeposits(ctx context.Context, requesterRole, requesterID, agencyID, status, search string, page, limit int64) ([]*domain.ImportedDepositView, int64, error) {
	agencyID, err := s.resolveSyncAgency(ctx, requesterRole, requesterID, agencyID)
	if err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	switch status {
	case domain.ImportedDepositStatusUnclaimed, domain.ImportedDepositStatusLinked, domain.ImportedDepositStatusAll:
	default:
		status = domain.ImportedDepositStatusAll
	}

	return s.importedDepositRepo.FindAll(ctx, agencyID, status, strings.TrimSpace(search), page, limit)
}

// LinkImportedDeposit creates the client's Fixed Deposit record from an inbox row.
func (s *agencySyncService) LinkImportedDeposit(ctx context.Context, requesterRole, requesterID, agencyID, idStr string, dto *domain.LinkImportedDepositDTO) (*domain.FixedDeposit, error) {
	agencyID, err := s.resolveSyncAgency(ctx, requesterRole, requesterID, agencyID)
	if err != nil {
		return nil, err
	}

	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid imported deposit ID format: %w", err)
	}

	imported, err := s.importedDepositRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if imported.AgencyID != agencyID {
		return nil, fmt.Errorf("imported deposit not found")
	}

	userID, err := bson.ObjectIDFromHex(dto.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid client ID format: %w", err)
	}

	targetUser, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || targetUser == nil {
		return nil, fmt.Errorf("client account not found")
	}
	// Checked against the agency being acted for, not against the caller's role. A super_admin
	// importing on behalf of one agency must not be able to attach that agency's row to another
	// agency's client — the row and the client have to belong to the same book, which is the same
	// rule an admin follows.
	if targetUser.AgencyID == "" || targetUser.AgencyID != agencyID {
		return nil, fmt.Errorf("client account not found")
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
		return nil, fmt.Errorf("family member does not belong to the selected client")
	}

	maturityDate := imported.MaturityDate
	if dto.MaturityDate != nil && !dto.MaturityDate.IsZero() {
		maturityDate = dto.MaturityDate.UTC()
	}
	if maturityDate.IsZero() {
		return nil, fmt.Errorf("this account has no maturity date on the report — enter one to link it")
	}
	if !imported.IssueDate.Before(maturityDate) {
		return nil, fmt.Errorf("maturity date must be after the issue date (%s)", imported.IssueDate.Format("02 Jan 2006"))
	}

	termMonths := imported.TermMonths
	if termMonths <= 0 {
		termMonths = int(maturityDate.Sub(imported.IssueDate).Hours()/24/30.44 + 0.5)
	}
	if termMonths <= 0 {
		termMonths = 1
	}

	name := strings.TrimSpace(dto.FDName)
	if name == "" {
		name = strings.TrimSpace(imported.Scheme)
	}
	if name == "" {
		name = "Post office deposit"
	}

	company := strings.TrimSpace(dto.CompanyName)
	if company == "" {
		company = defaultDepositInstitution
	}

	address := strings.TrimSpace(dto.Address)
	if address == "" {
		address = "Post Office"
	}

	deposit := &domain.FixedDeposit{
		UserID:          userID,
		FamilyMemberID:  familyMemberID,
		FDNumber:        imported.AccountNo,
		FDName:          name,
		CompanyName:     company,
		PrincipalAmount: imported.DepositAmount,
		// A monthly-income scheme pays interest out each month and returns the principal at
		// maturity, so its maturity value is the principal — not zero, which is what the report's
		// blank maturity column would otherwise leave on the client's portfolio.
		MaturityAmount:   maturityValueForImported(imported),
		MonthlyIncome:    imported.MonthlyIncome,
		Term:             termMonths,
		OpeningDate:      imported.IssueDate,
		MaturityDate:     maturityDate,
		NomineeName:      strings.TrimSpace(dto.NomineeName),
		SecondHolderName: strings.TrimSpace(imported.JointHolderName),
		AccountType:      postalAccountTypeFor(imported),
		Address:          address,
		IsMapped:         true,
		ManagedBy:        domain.ManagedByAgency,
	}

	created, err := s.fixedDepositRepo.Create(ctx, deposit)
	if err != nil {
		return nil, err
	}

	return created, nil
}

// DeleteImportedDeposit removes an inbox row that no client deposit is built on.
func (s *agencySyncService) DeleteImportedDeposit(ctx context.Context, requesterRole, requesterID, agencyID, idStr string) error {
	agencyID, err := s.resolveSyncAgency(ctx, requesterRole, requesterID, agencyID)
	if err != nil {
		return err
	}

	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid imported deposit ID format: %w", err)
	}

	imported, err := s.importedDepositRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if imported.AgencyID != agencyID {
		return fmt.Errorf("imported deposit not found")
	}

	existing, err := s.fixedDepositRepo.GetExistingFDNumbers(ctx, []string{imported.AccountNo}, agencyID)
	if err != nil {
		return fmt.Errorf("failed to check whether this deposit is linked: %w", err)
	}
	if existing[imported.AccountNo] {
		return fmt.Errorf("this account is linked to a client — remove it from that client's deposits first")
	}

	return s.importedDepositRepo.Delete(ctx, id)
}

// maturityValueFor is what the client should see as the deposit's value at maturity.
func maturityValueFor(rec PostalParsedRecord) float64 {
	if rec.MaturityAmount > 0 {
		return rec.MaturityAmount
	}
	if rec.MonthlyIncome > 0 {
		return rec.DepositAmount
	}
	return 0
}

func maturityValueForImported(rec *domain.ImportedDeposit) float64 {
	if rec.MaturityAmount > 0 {
		return rec.MaturityAmount
	}
	if rec.MonthlyIncome > 0 {
		return rec.DepositAmount
	}
	return rec.DepositAmount
}

// postalAccountTypeFor labels the stored deposit, e.g. "Post Office — MIS".
func postalAccountTypeFor(rec *domain.ImportedDeposit) string {
	scheme := rec.SchemeCode
	if scheme == "" {
		scheme = rec.Scheme
	}
	if scheme == "" {
		return "Post Office"
	}
	return "Post Office — " + scheme
}
