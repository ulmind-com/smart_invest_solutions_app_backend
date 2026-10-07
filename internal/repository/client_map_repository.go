package repository

import (
	"context"
	"fmt"
	"regexp"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	healthInsurancesCollection  = "health_insurances"
	generalInsurancesCollection = "general_insurances"
)

// clientMapRepository reads the client map straight out of the users collection, joining each
// client's holdings and their agency admin in one round trip.
type clientMapRepository struct {
	collection *mongo.Collection
}

// NewClientMapRepository initializes a new ClientMapRepository.
func NewClientMapRepository(db *mongo.Database) domain.ClientMapRepository {
	return &clientMapRepository{collection: db.Collection(usersCollection)}
}

// FindClientMap answers the whole screen in a single aggregation.
//
// The shape matters: a client list with four holding counts each is the classic N+1 — 50 clients
// would be 200 extra queries per page. Instead every count is a $lookup whose sub-pipeline groups
// on the server, so each join returns at most one document per client, and a $facet computes the
// page, its total and the summary over the whole filtered scope at once. The summary deliberately
// ignores the status/holdings filters so the tallies stay still while the reader toggles them.
func (r *clientMapRepository) FindClientMap(ctx context.Context, query domain.ClientMapQuery) (*domain.ClientMapResult, error) {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: clientMapScope(query)}},
		clientMapCountLookup(lifeInsurancesCollection, "life"),
		clientMapCountLookup(healthInsurancesCollection, "health"),
		clientMapCountLookup(generalInsurancesCollection, "motor"),
		clientMapCountLookup(fixedDepositsCollection, "deposit"),
		clientMapAgencyLookup(),
		clientMapCountFields(),
		clientMapDerivedFields(),
		clientMapProjection(),
		clientMapFacet(query),
	}

	// A super_admin's unfiltered view sorts every client on the platform; allowing disk keeps that
	// from failing on the aggregation's in-memory limit as the platform grows.
	cursor, err := r.collection.Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(true))
	if err != nil {
		return nil, fmt.Errorf("failed to build the client map: %w", err)
	}
	defer cursor.Close(ctx)

	var faceted []struct {
		Rows    []*domain.ClientMapRow    `bson:"rows"`
		Summary []domain.ClientMapSummary `bson:"summary"`
		Count   []struct {
			N int64 `bson:"n"`
		} `bson:"count"`
	}
	if err := cursor.All(ctx, &faceted); err != nil {
		return nil, fmt.Errorf("failed to decode the client map: %w", err)
	}

	result := &domain.ClientMapResult{
		Items:   []*domain.ClientMapRow{},
		Summary: &domain.ClientMapSummary{},
	}
	// A $facet always yields exactly one document, but an empty scope yields empty facets inside it,
	// so every branch below has to tolerate "nothing matched" without reporting an error.
	if len(faceted) == 0 {
		return result, nil
	}
	if len(faceted[0].Rows) > 0 {
		result.Items = faceted[0].Rows
	}
	if len(faceted[0].Summary) > 0 {
		summary := faceted[0].Summary[0]
		result.Summary = &summary
	}
	if len(faceted[0].Count) > 0 {
		result.Total = faceted[0].Count[0].N
	}

	return result, nil
}

// clientMapScope narrows the collection to the clients this request is about: never staff accounts,
// never an account retired by a family merge (it can't sign in and its data has moved), and within
// one agency when one was asked for.
func clientMapScope(query domain.ClientMapQuery) bson.D {
	scope := bson.D{
		{Key: "role", Value: domain.RoleClient},
		{Key: "merged_into_user_id", Value: nil},
	}

	switch query.AgencyID {
	case "":
		// Every agency.
	case domain.ClientMapAgencyUnassigned:
		// Written either way round over the platform's history: missing, or explicitly blank.
		scope = append(scope, bson.E{Key: "$or", Value: bson.A{
			bson.D{{Key: "agency_id", Value: nil}},
			bson.D{{Key: "agency_id", Value: ""}},
		}})
	default:
		scope = append(scope, bson.E{Key: "agency_id", Value: query.AgencyID})
	}

	if query.Search != "" {
		// Escaped: the search box is a plain substring match, never a regex.
		safe := regexp.QuoteMeta(query.Search)
		scope = append(scope, bson.E{Key: "$and", Value: bson.A{
			bson.D{{Key: "$or", Value: bson.A{
				bson.D{{Key: "name", Value: bson.D{{Key: "$regex", Value: safe}, {Key: "$options", Value: "i"}}}},
				bson.D{{Key: "email", Value: bson.D{{Key: "$regex", Value: safe}, {Key: "$options", Value: "i"}}}},
				bson.D{{Key: "phone", Value: bson.D{{Key: "$regex", Value: safe}, {Key: "$options", Value: "i"}}}},
			}}},
		}})
	}

	return scope
}

// clientMapCountLookup joins one holdings collection and reduces it, on the server, to a single
// document per client: how many records they hold there and how many of those the agency maintains.
func clientMapCountLookup(from, as string) bson.D {
	return bson.D{{Key: "$lookup", Value: bson.D{
		{Key: "from", Value: from},
		{Key: "let", Value: bson.D{{Key: "clientID", Value: "$_id"}}},
		{Key: "pipeline", Value: mongo.Pipeline{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "$expr", Value: bson.D{{Key: "$eq", Value: bson.A{"$user_id", "$$clientID"}}}},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: nil},
				{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
				{Key: "agency", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{
					bson.D{{Key: "$eq", Value: bson.A{"$managed_by", domain.ManagedByAgency}}},
					1,
					0,
				}}}}}},
			}}},
		}},
		{Key: "as", Value: as},
	}}}
}

// clientMapAgencyLookup names the admin behind the client's Agency ID — the "which admin is this
// client under?" column.
//
// The join is expressed as a sub-pipeline rather than localField/foreignField on purpose: a client
// with no agency has an empty agency_id, which under a plain field join would match every document
// that has no admin_id either — i.e. every other client.
func clientMapAgencyLookup() bson.D {
	return bson.D{{Key: "$lookup", Value: bson.D{
		{Key: "from", Value: usersCollection},
		{Key: "let", Value: bson.D{
			{Key: "agency", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$agency_id", ""}}}},
		}},
		{Key: "pipeline", Value: mongo.Pipeline{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "$expr", Value: bson.D{{Key: "$and", Value: bson.A{
					bson.D{{Key: "$ne", Value: bson.A{"$$agency", ""}}},
					bson.D{{Key: "$eq", Value: bson.A{"$admin_id", "$$agency"}}},
				}}}},
			}}},
			bson.D{{Key: "$limit", Value: 1}},
			bson.D{{Key: "$project", Value: bson.D{{Key: "name", Value: 1}}}},
		}},
		{Key: "as", Value: "agency_admin"},
	}}}
}

// clientMapCountFields flattens each lookup's single grouped document into plain numbers.
func clientMapCountFields() bson.D {
	fields := bson.D{}
	for _, name := range []string{"life", "health", "motor", "deposit"} {
		fields = append(fields,
			bson.E{Key: name + "_count", Value: clientMapLookupNumber(name, "n")},
			bson.E{Key: name + "_agency", Value: clientMapLookupNumber(name, "agency")},
		)
	}
	return bson.D{{Key: "$addFields", Value: fields}}
}

// clientMapLookupNumber reads one number out of a count lookup's result, defaulting to 0 for a
// client who holds nothing in that collection (where the lookup returns an empty array).
func clientMapLookupNumber(lookup, field string) bson.D {
	return bson.D{{Key: "$ifNull", Value: bson.A{
		bson.D{{Key: "$arrayElemAt", Value: bson.A{"$" + lookup + "." + field, 0}}},
		0,
	}}}
}

// clientMapDerivedFields adds the totals and the app-presence verdict. It is a separate stage from
// clientMapCountFields because a $addFields stage cannot read the fields it is itself adding.
func clientMapDerivedFields() bson.D {
	return bson.D{{Key: "$addFields", Value: bson.D{
		{Key: "policy_count", Value: bson.D{{Key: "$add", Value: bson.A{"$life_count", "$health_count", "$motor_count"}}}},
		{Key: "total_count", Value: bson.D{{Key: "$add", Value: bson.A{"$life_count", "$health_count", "$motor_count", "$deposit_count"}}}},
		{Key: "agency_managed_count", Value: bson.D{{Key: "$add", Value: bson.A{"$life_agency", "$health_agency", "$motor_agency", "$deposit_agency"}}}},
		// Order matters: an account that can't sign in is reported as such whether or not it ever
		// did, because "install the app" is not the action that would fix it.
		{Key: "app_status", Value: bson.D{{Key: "$switch", Value: bson.D{
			{Key: "branches", Value: bson.A{
				bson.D{
					{Key: "case", Value: bson.D{{Key: "$ne", Value: bson.A{
						bson.D{{Key: "$ifNull", Value: bson.A{"$is_active", false}}},
						true,
					}}}},
					{Key: "then", Value: domain.AppPresenceNoAccess},
				},
				bson.D{
					// Tested by type, not against null: in an aggregation expression a field that is
					// absent altogether is "missing" rather than null, so {$ne: [path, null]} is true
					// for every account that has never signed in — which would report the whole client
					// base as being on the app.
					{Key: "case", Value: bson.D{{Key: "$eq", Value: bson.A{
						bson.D{{Key: "$type", Value: "$last_login_at"}},
						"date",
					}}}},
					{Key: "then", Value: domain.AppPresenceOnApp},
				},
			}},
			{Key: "default", Value: domain.AppPresenceNotOnApp},
		}}}},
	}}}
}

// clientMapProjection shapes one row of the table and drops everything the screen doesn't read.
func clientMapProjection() bson.D {
	return bson.D{{Key: "$project", Value: bson.D{
		{Key: "_id", Value: 0},
		{Key: "user_id", Value: bson.D{{Key: "$toString", Value: "$_id"}}},
		{Key: "name", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$name", ""}}}},
		{Key: "email", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$email", ""}}}},
		{Key: "phone", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$phone", ""}}}},
		{Key: "agency_id", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$agency_id", ""}}}},
		{Key: "agency_name", Value: bson.D{{Key: "$ifNull", Value: bson.A{
			bson.D{{Key: "$arrayElemAt", Value: bson.A{"$agency_admin.name", 0}}},
			"",
		}}}},
		{Key: "is_active", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$is_active", false}}}},
		{Key: "is_email_verified", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$is_email_verified", false}}}},
		{Key: "last_login_at", Value: 1},
		{Key: "login_count", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$login_count", 0}}}},
		{Key: "app_status", Value: 1},
		{Key: "life_count", Value: 1},
		{Key: "health_count", Value: 1},
		{Key: "motor_count", Value: 1},
		{Key: "deposit_count", Value: 1},
		{Key: "policy_count", Value: 1},
		{Key: "total_count", Value: 1},
		{Key: "agency_managed_count", Value: 1},
		{Key: "created_at", Value: 1},
		// Sorted on a case-folded copy of the name so the table reads like a directory instead of
		// listing every lowercase name after every capitalised one.
		{Key: "sort_key", Value: bson.D{{Key: "$toLower", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$name", ""}}}}}},
	}}}
}

// clientMapFacet splits the shaped rows three ways: the summary over the whole scope, the page the
// reader asked for, and how many rows that page is drawn from.
func clientMapFacet(query domain.ClientMapQuery) bson.D {
	filters := clientMapRowFilters(query)

	rows := append(mongo.Pipeline{}, filters...)
	rows = append(rows,
		bson.D{{Key: "$sort", Value: bson.D{{Key: "sort_key", Value: 1}, {Key: "user_id", Value: 1}}}},
		bson.D{{Key: "$skip", Value: (query.Page - 1) * query.Limit}},
		bson.D{{Key: "$limit", Value: query.Limit}},
		bson.D{{Key: "$project", Value: bson.D{{Key: "sort_key", Value: 0}}}},
	)

	count := append(mongo.Pipeline{}, filters...)
	count = append(count, bson.D{{Key: "$count", Value: "n"}})

	return bson.D{{Key: "$facet", Value: bson.D{
		{Key: "summary", Value: mongo.Pipeline{clientMapSummaryGroup()}},
		{Key: "rows", Value: rows},
		{Key: "count", Value: count},
	}}}
}

// clientMapRowFilters are the filters that narrow the page but not the summary.
func clientMapRowFilters(query domain.ClientMapQuery) mongo.Pipeline {
	stages := mongo.Pipeline{}

	if query.AppStatus != "" {
		stages = append(stages, bson.D{{Key: "$match", Value: bson.D{
			{Key: "app_status", Value: query.AppStatus},
		}}})
	}

	switch query.Holdings {
	case domain.ClientMapHoldingsWith:
		stages = append(stages, bson.D{{Key: "$match", Value: bson.D{
			{Key: "total_count", Value: bson.D{{Key: "$gt", Value: 0}}},
		}}})
	case domain.ClientMapHoldingsWithout:
		stages = append(stages, bson.D{{Key: "$match", Value: bson.D{
			{Key: "total_count", Value: 0},
		}}})
	}

	return stages
}

// clientMapSummaryGroup tallies the whole scope in one pass.
func clientMapSummaryGroup() bson.D {
	countWhen := func(expr bson.D) bson.D {
		return bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{expr, 1, 0}}}}}
	}
	statusIs := func(status string) bson.D {
		return bson.D{{Key: "$eq", Value: bson.A{"$app_status", status}}}
	}

	return bson.D{{Key: "$group", Value: bson.D{
		{Key: "_id", Value: nil},
		{Key: "clients", Value: bson.D{{Key: "$sum", Value: 1}}},
		{Key: "on_app", Value: countWhen(statusIs(domain.AppPresenceOnApp))},
		{Key: "not_on_app", Value: countWhen(statusIs(domain.AppPresenceNotOnApp))},
		{Key: "no_access", Value: countWhen(statusIs(domain.AppPresenceNoAccess))},
		{Key: "without_holdings", Value: countWhen(bson.D{{Key: "$eq", Value: bson.A{"$total_count", 0}}})},
		{Key: "unassigned", Value: countWhen(bson.D{{Key: "$eq", Value: bson.A{"$agency_id", ""}}})},
		{Key: "policies", Value: bson.D{{Key: "$sum", Value: "$policy_count"}}},
		{Key: "deposits", Value: bson.D{{Key: "$sum", Value: "$deposit_count"}}},
	}}}
}
