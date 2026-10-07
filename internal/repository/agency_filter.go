package repository

import (
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// agencyMatchStage is the $match that narrows a record set to one agency, by the owning customer's
// agency_id (joined in as "customer" by the caller's pipeline).
//
// The value domain.ClientMapAgencyUnassigned asks for the opposite question — the records whose owner
// belongs to no agency at all, which is a super admin's housekeeping list. Those documents were
// written either way round over the platform's history (field missing, or present and blank), so both
// are matched.
func agencyMatchStage(agencyID string) bson.D {
	if agencyID == domain.ClientMapAgencyUnassigned {
		return bson.D{{Key: "$match", Value: bson.D{
			{Key: "$or", Value: bson.A{
				bson.D{{Key: "customer.agency_id", Value: nil}},
				bson.D{{Key: "customer.agency_id", Value: ""}},
			}},
		}}}
	}
	return bson.D{{Key: "$match", Value: bson.D{{Key: "customer.agency_id", Value: agencyID}}}}
}
