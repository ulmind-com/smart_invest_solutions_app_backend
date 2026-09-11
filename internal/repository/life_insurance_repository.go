package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type lifeInsuranceRepository struct {
	collection *mongo.Collection
}

// NewLifeInsuranceRepository initializes a new LifeInsuranceRepository.
func NewLifeInsuranceRepository(db *mongo.Database) domain.LifeInsuranceRepository {
	col := db.Collection("life_insurances")

	// Unique index on policy number — prevents the same policy being recorded twice
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "policy_details.policy_no", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	// Ascending index on next due date — optimizes future premium reminder/dashboard queries
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "premium_details.next_due_date", Value: 1}},
	})

	// Index on user_id for fast per-client queries
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}},
	})

	return &lifeInsuranceRepository{collection: col}
}

// Create inserts a new life insurance policy into MongoDB.
func (r *lifeInsuranceRepository) Create(ctx context.Context, policy *domain.LifeInsurance) (*domain.LifeInsurance, error) {
	now := time.Now().UTC()
	policy.CreatedAt = now
	policy.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, policy)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("a policy with this policy number already exists")
		}
		return nil, fmt.Errorf("failed to insert life insurance policy: %w", err)
	}

	policy.ID = result.InsertedID.(bson.ObjectID)
	return policy, nil
}

// GetByID retrieves a single life insurance policy by ID.
func (r *lifeInsuranceRepository) GetByID(ctx context.Context, id bson.ObjectID) (*domain.LifeInsurance, error) {
	var policy domain.LifeInsurance
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&policy)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("life insurance policy not found")
		}
		return nil, fmt.Errorf("failed to find life insurance policy: %w", err)
	}
	return &policy, nil
}

// GetByUserID retrieves all life insurance policies belonging to a specific client.
func (r *lifeInsuranceRepository) GetByUserID(ctx context.Context, userID bson.ObjectID) ([]*domain.LifeInsurance, int64, error) {
	filter := bson.M{"user_id": userID}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query life insurance policies: %w", err)
	}
	defer cursor.Close(ctx)

	var policies []*domain.LifeInsurance
	if err := cursor.All(ctx, &policies); err != nil {
		return nil, 0, fmt.Errorf("failed to decode life insurance policies: %w", err)
	}

	if policies == nil {
		policies = []*domain.LifeInsurance{}
	}

	return policies, int64(len(policies)), nil
}

// GetAll retrieves a paginated master list of every life insurance policy across all clients,
// optionally filtered by is_mapped, the insured family member's LIC Customer ID, and/or the owning
// customer's Agency ID (agencyID — empty means no restriction, used to scope a plain admin's view
// to their own agency), each row enriched (via $lookup) with the owning customer's name/contact/
// agency and that Customer ID — the latter is looked up live from family_members every call, never
// cached, so it can never go stale.
func (r *lifeInsuranceRepository) GetAll(ctx context.Context, page, limit int64, isMapped *bool, licCustomerID, agencyID string) ([]*domain.LifeInsuranceWithCustomer, int64, error) {
	skip := (page - 1) * limit

	basePipeline := mongo.Pipeline{}
	if isMapped != nil {
		basePipeline = append(basePipeline, bson.D{{Key: "$match", Value: bson.D{{Key: "is_mapped", Value: *isMapped}}}})
	}
	if licCustomerID != "" {
		basePipeline = append(basePipeline,
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
			bson.D{{Key: "$match", Value: bson.D{{Key: "family.lic_customer_id", Value: licCustomerID}}}},
		)
	}
	// The customer lookup moves ahead of pagination (rather than staying in the page-only section
	// below) whenever it's needed to filter by agency — count and page must agree on which agency
	// the record belongs to.
	if agencyID != "" {
		basePipeline = append(basePipeline,
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
			bson.D{{Key: "$match", Value: bson.D{{Key: "customer.agency_id", Value: agencyID}}}},
		)
	}

	countPipeline := append(mongo.Pipeline{}, basePipeline...)
	countPipeline = append(countPipeline, bson.D{{Key: "$count", Value: "total"}})

	countCursor, err := r.collection.Aggregate(ctx, countPipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count life insurance policies: %w", err)
	}
	defer countCursor.Close(ctx)

	var countResult []struct {
		Total int64 `bson:"total"`
	}
	if err := countCursor.All(ctx, &countResult); err != nil {
		return nil, 0, fmt.Errorf("failed to decode life insurance policy count: %w", err)
	}
	var total int64
	if len(countResult) > 0 {
		total = countResult[0].Total
	}

	pipeline := append(mongo.Pipeline{}, basePipeline...)
	pipeline = append(pipeline,
		bson.D{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}}}},
		bson.D{{Key: "$skip", Value: skip}},
		bson.D{{Key: "$limit", Value: limit}},
	)
	// The customer lookup above only runs in basePipeline when filtering by agency; when it
	// doesn't, it still needs to happen here so every row gets customer_name/contact/agency_id.
	if agencyID == "" {
		pipeline = append(pipeline,
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
		)
	}
	// The family lookup above only runs when filtering by Customer ID; when it doesn't, this
	// second lookup still supplies lic_customer_id for display purposes on every row.
	if licCustomerID == "" {
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
	pipeline = append(pipeline,
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "user_id", Value: 1},
			{Key: "family_member_id", Value: 1},
			{Key: "company_name", Value: 1},
			{Key: "customer_name", Value: "$customer.name"},
			{Key: "contact_no", Value: "$customer.phone"},
			{Key: "agency_id", Value: "$customer.agency_id"},
			{Key: "lic_customer_id", Value: "$family.lic_customer_id"},
			{Key: "policy_details", Value: 1},
			{Key: "premium_details", Value: 1},
			{Key: "is_mapped", Value: 1},
			{Key: "created_at", Value: 1},
			{Key: "updated_at", Value: 1},
		}}},
	)

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query life insurance policies: %w", err)
	}
	defer cursor.Close(ctx)

	var policies []*domain.LifeInsuranceWithCustomer
	if err := cursor.All(ctx, &policies); err != nil {
		return nil, 0, fmt.Errorf("failed to decode life insurance policies: %w", err)
	}

	if policies == nil {
		policies = []*domain.LifeInsuranceWithCustomer{}
	}

	return policies, total, nil
}

// Update modifies an existing life insurance policy. Ownership/RBAC is enforced by the service
// layer before this is called, so the filter here is by ID alone.
func (r *lifeInsuranceRepository) Update(ctx context.Context, id bson.ObjectID, dto *domain.UpdateLifeInsuranceDTO) (*domain.LifeInsurance, error) {
	updateFields := bson.M{
		"updated_at": time.Now().UTC(),
	}

	if dto.FamilyMemberID != nil {
		familyMemberID, err := bson.ObjectIDFromHex(*dto.FamilyMemberID)
		if err != nil {
			return nil, fmt.Errorf("invalid family member ID format: %w", err)
		}
		updateFields["family_member_id"] = familyMemberID
	}
	if dto.CompanyName != nil {
		updateFields["company_name"] = *dto.CompanyName
	}
	if dto.PolicyNo != nil {
		updateFields["policy_details.policy_no"] = *dto.PolicyNo
	}
	if dto.PlanName != nil {
		updateFields["policy_details.plan_name"] = *dto.PlanName
	}
	if dto.LifeInsuredName != nil {
		updateFields["policy_details.life_insured_name"] = *dto.LifeInsuredName
	}
	if dto.NomineeName != nil {
		updateFields["policy_details.nominee_name"] = *dto.NomineeName
	}
	if dto.SumAssured != nil {
		updateFields["policy_details.sum_assured"] = *dto.SumAssured
	}
	if dto.Term != nil {
		updateFields["policy_details.term"] = *dto.Term
	}
	if dto.PPT != nil {
		updateFields["policy_details.ppt"] = *dto.PPT
	}
	if dto.DOC != nil {
		updateFields["policy_details.doc"] = *dto.DOC
	}
	if dto.MaturityDate != nil {
		updateFields["policy_details.maturity_date"] = *dto.MaturityDate
	}
	if dto.InstallmentPremium != nil {
		updateFields["premium_details.installment_premium"] = *dto.InstallmentPremium
	}
	if dto.NextDueDate != nil {
		updateFields["premium_details.next_due_date"] = *dto.NextDueDate
	}
	if dto.PaymentMode != nil {
		updateFields["premium_details.payment_mode"] = *dto.PaymentMode
	}
	if dto.IsMapped != nil {
		updateFields["is_mapped"] = *dto.IsMapped
	}

	filter := bson.M{"_id": id}
	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedPolicy domain.LifeInsurance
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedPolicy)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("life insurance policy not found")
		}
		if mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("a policy with this policy number already exists")
		}
		return nil, fmt.Errorf("failed to update life insurance policy: %w", err)
	}

	return &updatedPolicy, nil
}

// Delete removes a life insurance policy record by ID.
func (r *lifeInsuranceRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete life insurance policy: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("life insurance policy not found")
	}
	return nil
}

// DeleteAllByUserID deletes all life insurance policy records belonging to a user.
func (r *lifeInsuranceRepository) DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}

// ReassignOwner moves every policy owned by fromUserID to toUserID — used by MergeFamilyAccounts.
func (r *lifeInsuranceRepository) ReassignOwner(ctx context.Context, fromUserID, toUserID bson.ObjectID) (int64, error) {
	result, err := r.collection.UpdateMany(ctx,
		bson.M{"user_id": fromUserID},
		bson.M{"$set": bson.M{"user_id": toUserID, "updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to reassign life insurance policies: %w", err)
	}
	return result.ModifiedCount, nil
}

// agencyUserIDs looks up every user belonging to agencyID and returns their ObjectIDs — used to
// scope a bulk sync write/lookup to only the calling admin's own clients. Returns (nil, true) when
// agencyID is empty, meaning "no restriction" (the caller must handle that case itself).
func (r *lifeInsuranceRepository) agencyUserIDs(ctx context.Context, agencyID string) ([]bson.ObjectID, bool, error) {
	if agencyID == "" {
		return nil, true, nil
	}
	cursor, err := r.collection.Database().Collection(usersCollection).Find(ctx,
		bson.M{"agency_id": agencyID},
		options.Find().SetProjection(bson.M{"_id": 1}),
	)
	if err != nil {
		return nil, false, fmt.Errorf("failed to resolve agency clients: %w", err)
	}
	defer cursor.Close(ctx)

	var ids []bson.ObjectID
	for cursor.Next(ctx) {
		var doc struct {
			ID bson.ObjectID `bson:"_id"`
		}
		if err := cursor.Decode(&doc); err == nil {
			ids = append(ids, doc.ID)
		}
	}
	return ids, false, nil
}

// BulkUpdateFromSync performs bulk updates of life insurance policies from parsed LIC sync records.
// It performs an unordered bulk write, so a failure on one record does not block the rest: the
// modified count and exactly which policy numbers failed (with MongoDB's reason for each) are both
// returned, so an admin can see and diagnose individual failures instead of just a bare count. err
// is only set for failures affecting the whole operation (e.g. the write itself couldn't be
// attempted at all). agencyID, when non-empty, restricts every write to policies owned by that
// agency's clients — a plain admin's sync run can never touch another agency's policies even if a
// policy number in their uploaded PDF happens to coincide with one belonging to a different agency.
func (r *lifeInsuranceRepository) BulkUpdateFromSync(ctx context.Context, records []domain.LICParsedRecord, agencyID string) (int64, []domain.FailedSyncPolicy, error) {
	if len(records) == 0 {
		return 0, nil, nil
	}

	agencyUserIDs, unrestricted, err := r.agencyUserIDs(ctx, agencyID)
	if err != nil {
		return 0, nil, err
	}
	if !unrestricted && len(agencyUserIDs) == 0 {
		// The calling admin's agency has no clients at all — nothing in this PDF can possibly
		// belong to them, so every record is a no-op failure rather than a silent global match.
		failed := make([]domain.FailedSyncPolicy, len(records))
		for i, rec := range records {
			failed[i] = domain.FailedSyncPolicy{PolicyNo: rec.PolicyNo, Reason: "no client found under your agency"}
		}
		return 0, failed, nil
	}

	var models []mongo.WriteModel
	now := time.Now().UTC()

	for _, rec := range records {
		updateFields := bson.M{
			"premium_details.next_due_date":       rec.CalculatedNextDueDate,
			"premium_details.installment_premium": rec.Premium,
			"premium_details.payment_mode":        mapModeToDomainMode(rec.Mode),
			"updated_at":                          now,
		}

		if docTime, err := time.Parse("02/01/2006", rec.DOC); err == nil && !docTime.IsZero() {
			updateFields["policy_details.doc"] = docTime.UTC()
		}

		filter := bson.M{"policy_details.policy_no": rec.PolicyNo}
		if !unrestricted {
			filter["user_id"] = bson.M{"$in": agencyUserIDs}
		}

		model := mongo.NewUpdateOneModel().
			SetFilter(filter).
			SetUpdate(bson.M{"$set": updateFields})

		models = append(models, model)
	}

	opts := options.BulkWrite().SetOrdered(false)
	result, err := r.collection.BulkWrite(ctx, models, opts)
	if err != nil {
		var bulkErr mongo.BulkWriteException
		if errors.As(err, &bulkErr) {
			// Unordered bulk write: some records failed individually but the operation as a
			// whole succeeded. result is still populated for the records that did succeed. Each
			// WriteError.Index is the position in `models` (and therefore `records`) that failed,
			// which the driver remaps back to the original request index even across batches.
			failed := make([]domain.FailedSyncPolicy, 0, len(bulkErr.WriteErrors))
			for _, we := range bulkErr.WriteErrors {
				policyNo := "unknown"
				if we.Index >= 0 && we.Index < len(records) {
					policyNo = records[we.Index].PolicyNo
				}
				failed = append(failed, domain.FailedSyncPolicy{PolicyNo: policyNo, Reason: we.Message})
			}
			return result.ModifiedCount, failed, nil
		}
		return 0, nil, fmt.Errorf("failed to bulk update life insurance policies: %w", err)
	}

	return result.ModifiedCount, nil, nil
}

// GetExistingPolicyNumbers checks MongoDB for existing policy numbers and returns a map of
// policy_no -> true. agencyID, when non-empty, restricts the check to policies owned by that
// agency's clients, so a policy number belonging to another agency is correctly reported as
// "not found" rather than leaking its existence across the agency boundary.
func (r *lifeInsuranceRepository) GetExistingPolicyNumbers(ctx context.Context, policyNos []string, agencyID string) (map[string]bool, error) {
	existingMap := make(map[string]bool)
	if len(policyNos) == 0 {
		return existingMap, nil
	}

	agencyUserIDs, unrestricted, err := r.agencyUserIDs(ctx, agencyID)
	if err != nil {
		return nil, err
	}
	if !unrestricted && len(agencyUserIDs) == 0 {
		return existingMap, nil
	}

	filter := bson.M{"policy_details.policy_no": bson.M{"$in": policyNos}}
	if !unrestricted {
		filter["user_id"] = bson.M{"$in": agencyUserIDs}
	}
	opts := options.Find().SetProjection(bson.M{"policy_details.policy_no": 1})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing policy numbers: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var doc struct {
			PolicyDetails struct {
				PolicyNo string `bson:"policy_no"`
			} `bson:"policy_details"`
		}
		if err := cursor.Decode(&doc); err == nil && doc.PolicyDetails.PolicyNo != "" {
			existingMap[doc.PolicyDetails.PolicyNo] = true
		}
	}

	return existingMap, nil
}

func mapModeToDomainMode(mode string) string {
	switch strings.TrimSpace(strings.ToUpper(mode)) {
	case "YLY", "Y", "YEARLY":
		return domain.PaymentModeYearly
	case "HLY", "H", "HALF-YEARLY", "HALF YEARLY":
		return domain.PaymentModeHalfYearly
	case "QLY", "Q", "QUARTERLY":
		return domain.PaymentModeQuarterly
	case "MLY", "M", "MONTHLY", "SSS":
		return domain.PaymentModeMonthly
	default:
		if mode != "" {
			return mode
		}
		return domain.PaymentModeYearly
	}
}
