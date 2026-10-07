package service

import (
	"context"
	"strings"

	"github.com/smart-invest-solutions/backend/internal/domain"
)

// Paging bounds for the client map. The default page is large because the point of the screen is to
// scan a table, not to click through it.
const (
	clientMapDefaultLimit = 25
	clientMapMaxLimit     = 100
)

// clientMapService implements domain.ClientMapService.
type clientMapService struct {
	clientMapRepo domain.ClientMapRepository
	userRepo      domain.UserRepository
}

// NewClientMapService creates the client map service.
func NewClientMapService(clientMapRepo domain.ClientMapRepository, userRepo domain.UserRepository) domain.ClientMapService {
	return &clientMapService{clientMapRepo: clientMapRepo, userRepo: userRepo}
}

// GetClientMap returns one page of the client map for this caller.
//
// Scoping is the whole point of this method. A super_admin is looking across the platform and may
// aim the view at one agency — or at the clients that belong to none. A plain admin is only ever
// looking at their own agency, so their own Agency ID replaces whatever the request asked for; an
// admin whose Agency ID can't be resolved sees nothing rather than everything.
func (s *clientMapService) GetClientMap(ctx context.Context, requesterRole, requesterID string, query domain.ClientMapQuery) (*domain.ClientMapResult, error) {
	query = normalizeClientMapQuery(query)

	switch requesterRole {
	case domain.RoleSuperAdmin:
		// Keeps the caller's agency filter as asked.
	case domain.RoleAdmin:
		agencyID := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if agencyID == "" {
			return emptyClientMap(), nil
		}
		query.AgencyID = agencyID
	default:
		// The router restricts this endpoint to agency staff; this is the belt to that braces, so a
		// future route change can't turn the whole client base into a client-readable list.
		return emptyClientMap(), nil
	}

	result, err := s.clientMapRepo.FindClientMap(ctx, query)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return emptyClientMap(), nil
	}
	if result.Items == nil {
		result.Items = []*domain.ClientMapRow{}
	}
	if result.Summary == nil {
		result.Summary = &domain.ClientMapSummary{}
	}

	return result, nil
}

// normalizeClientMapQuery clamps paging and drops filter values the aggregation wouldn't recognise,
// so an unknown value reads as "no filter" instead of silently matching nothing.
func normalizeClientMapQuery(query domain.ClientMapQuery) domain.ClientMapQuery {
	query.AgencyID = strings.TrimSpace(query.AgencyID)
	query.Search = strings.TrimSpace(query.Search)

	switch strings.TrimSpace(strings.ToLower(query.AppStatus)) {
	case domain.AppPresenceOnApp:
		query.AppStatus = domain.AppPresenceOnApp
	case domain.AppPresenceNotOnApp:
		query.AppStatus = domain.AppPresenceNotOnApp
	case domain.AppPresenceNoAccess:
		query.AppStatus = domain.AppPresenceNoAccess
	default:
		query.AppStatus = ""
	}

	switch strings.TrimSpace(strings.ToLower(query.Holdings)) {
	case domain.ClientMapHoldingsWith:
		query.Holdings = domain.ClientMapHoldingsWith
	case domain.ClientMapHoldingsWithout:
		query.Holdings = domain.ClientMapHoldingsWithout
	default:
		query.Holdings = ""
	}

	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit < 1 || query.Limit > clientMapMaxLimit {
		query.Limit = clientMapDefaultLimit
	}

	return query
}

// emptyClientMap is the "nothing to show" answer — never nil slices, so the app renders an empty
// table rather than failing to read a null.
func emptyClientMap() *domain.ClientMapResult {
	return &domain.ClientMapResult{
		Items:   []*domain.ClientMapRow{},
		Summary: &domain.ClientMapSummary{},
	}
}
