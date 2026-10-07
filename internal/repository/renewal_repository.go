package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// renewalRepository reads the records falling due inside the renewal horizon, one collection at a
// time, each with its owner and that owner's agency attached.
type renewalRepository struct {
	life     *mongo.Collection
	health   *mongo.Collection
	motor    *mongo.Collection
	deposits *mongo.Collection
}

// NewRenewalRepository initializes a new RenewalRepository.
func NewRenewalRepository(db *mongo.Database) domain.RenewalRepository {
	return &renewalRepository{
		life:     db.Collection(lifeInsurancesCollection),
		health:   db.Collection(healthInsurancesCollection),
		motor:    db.Collection(generalInsurancesCollection),
		deposits: db.Collection(fixedDepositsCollection),
	}
}

// renewalPipeline is the shape every instrument's query shares: narrow by the record's own due date
// first, then join the owner (and the insured family member where there is one), then narrow by the
// owner's agency.
//
// Date before owner on purpose: the date filter is what makes the result small, and doing it first
// means the joins only run for records that can actually appear in the list.
func renewalPipeline(dueFilter bson.D, agencyID string, withFamily bool) mongo.Pipeline {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: dueFilter}},
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: usersCollection},
			{Key: "localField", Value: "user_id"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "customer"},
		}}},
		bson.D{{Key: "$unwind", Value: bson.D{
			{Key: "path", Value: "$customer"},
			{Key: "preserveNullAndEmptyArrays", Value: true},
		}}},
	}

	if agencyID != "" {
		pipeline = append(pipeline, agencyMatchStage(agencyID))
	}

	if withFamily {
		pipeline = append(pipeline,
			bson.D{{Key: "$lookup", Value: bson.D{
				{Key: "from", Value: familyMembersCollection},
				{Key: "localField", Value: "family_member_id"},
				{Key: "foreignField", Value: "_id"},
				{Key: "as", Value: "family"},
			}}},
			bson.D{{Key: "$unwind", Value: bson.D{
				{Key: "path", Value: "$family"},
				{Key: "preserveNullAndEmptyArrays", Value: true},
			}}},
		)
	}

	return pipeline
}

// renewalOwnerFields are the owner columns every renewal row carries.
func renewalOwnerFields() bson.D {
	return bson.D{
		{Key: "user_id", Value: 1},
		{Key: "customer_name", Value: "$customer.name"},
		{Key: "contact_no", Value: "$customer.phone"},
		{Key: "agency_id", Value: "$customer.agency_id"},
		{Key: "managed_by", Value: 1},
	}
}

// LifeDue returns life policies whose next premium falls in the window.
func (r *renewalRepository) LifeDue(ctx context.Context, from, to time.Time, agencyID string) ([]*domain.LifeInsuranceWithCustomer, error) {
	pipeline := renewalPipeline(bson.D{
		{Key: "premium_details.next_due_date", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lte", Value: to}}},
	}, agencyID, true)

	project := bson.D{{Key: "_id", Value: 1}, {Key: "company_name", Value: 1}, {Key: "policy_details", Value: 1}, {Key: "premium_details", Value: 1}}
	project = append(project, renewalOwnerFields()...)
	pipeline = append(pipeline, bson.D{{Key: "$project", Value: project}})

	var rows []*domain.LifeInsuranceWithCustomer
	if err := renewalFetch(ctx, r.life, pipeline, &rows); err != nil {
		return nil, fmt.Errorf("failed to read life renewals: %w", err)
	}
	return rows, nil
}

// HealthDue returns health policies whose next premium falls in the window.
//
// The expiry date is projected too: a health policy renews by expiry as well as by premium, and the
// service decides which of the two to show.
func (r *renewalRepository) HealthDue(ctx context.Context, from, to time.Time, agencyID string) ([]*domain.HealthInsuranceWithCustomer, error) {
	pipeline := renewalPipeline(bson.D{
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "premium_details.next_due_date", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lte", Value: to}}}},
			bson.D{{Key: "policy_details.expiry_date", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lte", Value: to}}}},
		}},
	}, agencyID, true)

	project := bson.D{{Key: "_id", Value: 1}, {Key: "company_name", Value: 1}, {Key: "policy_details", Value: 1}, {Key: "premium_details", Value: 1}}
	project = append(project, renewalOwnerFields()...)
	pipeline = append(pipeline, bson.D{{Key: "$project", Value: project}})

	var rows []*domain.HealthInsuranceWithCustomer
	if err := renewalFetch(ctx, r.health, pipeline, &rows); err != nil {
		return nil, fmt.Errorf("failed to read health renewals: %w", err)
	}
	return rows, nil
}

// MotorExpiring returns motor policies expiring in the window.
//
// Motor expiry is stored as a "YYYY-MM-DD" string rather than a date. That format sorts and compares
// correctly as text, so the window is applied as a string range — no conversion, and the comparison
// still means what it says.
func (r *renewalRepository) MotorExpiring(ctx context.Context, fromISO, toISO, agencyID string) ([]*domain.GeneralInsuranceWithCustomer, error) {
	pipeline := renewalPipeline(bson.D{
		{Key: "date_of_expiry", Value: bson.D{{Key: "$gte", Value: fromISO}, {Key: "$lte", Value: toISO}}},
	}, agencyID, false)

	project := bson.D{
		{Key: "_id", Value: 1},
		{Key: "vehicle_no", Value: 1},
		{Key: "policy_no", Value: 1},
		{Key: "date_of_expiry", Value: 1},
		{Key: "company_name", Value: 1},
	}
	project = append(project, renewalOwnerFields()...)
	pipeline = append(pipeline, bson.D{{Key: "$project", Value: project}})

	var rows []*domain.GeneralInsuranceWithCustomer
	if err := renewalFetch(ctx, r.motor, pipeline, &rows); err != nil {
		return nil, fmt.Errorf("failed to read motor renewals: %w", err)
	}
	return rows, nil
}

// DepositsMaturing returns deposits maturing in the window.
func (r *renewalRepository) DepositsMaturing(ctx context.Context, from, to time.Time, agencyID string) ([]*domain.FixedDepositWithCustomer, error) {
	pipeline := renewalPipeline(bson.D{
		{Key: "maturity_date", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lte", Value: to}}},
	}, agencyID, true)

	project := bson.D{
		{Key: "_id", Value: 1},
		{Key: "fd_number", Value: 1},
		{Key: "fd_name", Value: 1},
		{Key: "company_name", Value: 1},
		{Key: "principal_amount", Value: 1},
		{Key: "maturity_amount", Value: 1},
		{Key: "maturity_date", Value: 1},
	}
	project = append(project, renewalOwnerFields()...)
	pipeline = append(pipeline, bson.D{{Key: "$project", Value: project}})

	var rows []*domain.FixedDepositWithCustomer
	if err := renewalFetch(ctx, r.deposits, pipeline, &rows); err != nil {
		return nil, fmt.Errorf("failed to read deposit maturities: %w", err)
	}
	return rows, nil
}

// renewalFetch runs one of the pipelines above and decodes it into out.
func renewalFetch(ctx context.Context, collection *mongo.Collection, pipeline mongo.Pipeline, out any) error {
	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	return cursor.All(ctx, out)
}
