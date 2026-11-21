package service

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/email"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type cronService struct {
	userRepo       domain.UserRepository
	userService    domain.UserService
	renewalService domain.RenewalService
	emailSvc       email.EmailService
}

// NewCronService creates a new instance of CronService.
func NewCronService(
	userRepo domain.UserRepository,
	userService domain.UserService,
	renewalService domain.RenewalService,
	emailSvc email.EmailService,
) domain.CronService {
	return &cronService{
		userRepo:       userRepo,
		userService:    userService,
		renewalService: renewalService,
		emailSvc:       emailSvc,
	}
}

// RunDailyMaintenance executes scheduled system maintenance:
// 1. Checks all admin accounts nearing expiration or already expired, and emails reminders (throttled).
// 2. Aggregates upcoming premium renewals across all policy types and deposits.
// 3. Produces a comprehensive JSON execution report with timing and task metrics.
func (s *cronService) RunDailyMaintenance(ctx context.Context) (*domain.CronExecutionResult, error) {
	start := time.Now()
	nowUTC := start.UTC()

	result := &domain.CronExecutionResult{
		Status:          "success",
		ExecutedAt:      nowUTC,
		RenewalsSummary: make(map[string]interface{}),
	}

	// Task 1: Check admin expiries & send automated warning alerts
	expiringAdmins, err := s.userService.ListExpiringAdmins(ctx, 2)
	if err != nil {
		log.Error().Err(err).Msg("[Cron] Failed to query expiring admins")
	} else {
		result.AdminAccountsChecked = len(expiringAdmins)
		for _, admin := range expiringAdmins {
			if admin.AdminExpiryDate != nil {
				if nowUTC.After(*admin.AdminExpiryDate) {
					result.ActiveAdminsExpired++
				}

				if alertErr := s.userService.SendAdminExpiryAlert(ctx, admin.ID.Hex()); alertErr != nil {
					// Could be cooldown (throttled) or email error
					log.Debug().Err(alertErr).Str("admin_id", admin.AdminID).Msg("[Cron] Admin expiry alert skipped or throttled")
				} else {
					result.AdminExpiryAlertsSent++
					log.Info().Str("admin_id", admin.AdminID).Msg("[Cron] Sent automated expiry alert email")
				}
			}
		}
	}

	// Task 2: Aggregated renewal book check
	if s.renewalService != nil {
		query := domain.RenewalQuery{
			Window: domain.RenewalWindowAll,
			Page:   1,
			Limit:  1,
		}
		renewals, err := s.renewalService.GetRenewals(ctx, domain.RoleSuperAdmin, "", query)
		if err != nil {
			log.Warn().Err(err).Msg("[Cron] Failed to compile renewals snapshot")
		} else if renewals != nil && renewals.Summary != nil {
			result.RenewalsSummary["overdue_count"] = renewals.Summary.Overdue
			result.RenewalsSummary["due_in_7_days"] = renewals.Summary.DueIn7
			result.RenewalsSummary["due_in_30_days"] = renewals.Summary.DueIn30
			result.RenewalsSummary["amount_overdue"] = renewals.Summary.AmountOverdue
			result.RenewalsSummary["amount_due_in_30"] = renewals.Summary.AmountDueIn30
			result.RenewalsSummary["total_life_due"] = renewals.Summary.Life
			result.RenewalsSummary["total_health_due"] = renewals.Summary.Health
			result.RenewalsSummary["total_motor_due"] = renewals.Summary.Motor
			result.RenewalsSummary["total_deposits_due"] = renewals.Summary.Deposits
		}
	}

	// Task 3: Send client renewal reminders for upcoming/overdue policies
	if s.emailSvc != nil && s.renewalService != nil {
		query := domain.RenewalQuery{
			Window: domain.RenewalWindow30,
			Page:   1,
			Limit:  200,
		}
		page, err := s.renewalService.GetRenewals(ctx, domain.RoleSuperAdmin, "", query)
		if err != nil {
			log.Warn().Err(err).Msg("[Cron] Failed to fetch renewals for client reminder dispatch")
		} else if page != nil && len(page.Items) > 0 {
			userCache := make(map[string]*domain.User)

			for _, row := range page.Items {
				// Send reminders for policies due in <= 7 days, due today (0), exactly 30 days, or overdue
				isReminderWindow := row.DaysLeft <= 7 || row.DaysLeft == 30 || row.IsOverdue
				if !isReminderWindow || row.ClientID == "" {
					continue
				}

				user, ok := userCache[row.ClientID]
				if !ok {
					clientObjID, parseErr := bson.ObjectIDFromHex(row.ClientID)
					if parseErr != nil {
						continue
					}
					u, findErr := s.userRepo.FindByID(ctx, clientObjID)
					if findErr == nil && u != nil {
						user = u
						userCache[row.ClientID] = u
					} else {
						userCache[row.ClientID] = nil
					}
				}

				if user != nil && user.Email != "" && user.IsActive {
					reminderData := email.RenewalReminderData{
						PolicyType:     row.Kind,
						PolicyNo:       row.Reference,
						PlanName:       row.Title,
						CompanyName:    row.Company,
						InsuredName:    row.InsuredName,
						AmountDue:      row.Amount,
						DueDate:        row.DueDate,
						DaysLeft:       row.DaysLeft,
						IsOverdue:      row.IsOverdue,
						PaymentMode:    row.Label,
						AdvisorName:    row.AgencyName,
						AdvisorContact: user.Phone,
					}

					if sendErr := s.emailSvc.SendRenewalReminderEmail(ctx, user.Email, user.Name, reminderData); sendErr != nil {
						log.Warn().Err(sendErr).Str("policy_no", row.Reference).Msg("[Cron] Failed to send renewal reminder email")
					} else {
						result.ClientRenewalAlertsSent++
						log.Info().Str("client", user.Email).Str("policy_no", row.Reference).Msg("[Cron] Sent automated renewal reminder email")
					}
				}
			}
		}
	}

	result.DurationMs = time.Since(start).Milliseconds()
	result.Message = "Daily maintenance completed successfully"

	log.Info().
		Int("alerts_sent", result.AdminExpiryAlertsSent).
		Int("admins_checked", result.AdminAccountsChecked).
		Int("admins_expired", result.ActiveAdminsExpired).
		Int("client_reminders_sent", result.ClientRenewalAlertsSent).
		Int64("duration_ms", result.DurationMs).
		Msg("[Cron] RunDailyMaintenance finished")

	return result, nil
}
