package repository

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const lifeInsurancesCollection = "life_insurances"

type importedPolicyRepository struct {
	collection *mongo.Collection
}

// NewImportedPolicyRepository initializes a new ImportedPolicyRepository.
func NewImportedPolicyRepository(db *mongo.Database) domain.ImportedPolicyRepository {
	col := db.Collection("imported_policies")

	// One row per policy number per agency — this is what makes re-uploading the same due list
	// idempotent instead of duplicating the agency's whole book on every sync.
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "agency_id", Value: 1}, {Key: "policy_no", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	// Supports the inbox listing, which always filters by agency first.
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "agency_id", Value: 1}, {Key: "next_due_date", Value: 1}},
	})

	return &importedPolicyRepository{collection: col}
}

// BulkUpsertFromSync writes one row per policy number, refreshing the PDF-sourced fields of rows
// that already exist. The write is split so that identity/first-seen fields are only stamped on
// insert: re-uploading next month's due list must update the money and dates without rewriting
// when the row first entered the book.
func (r *importedPolicyRepository) BulkUpsertFromSync(ctx context.Context, records []*domain.ImportedPolicy) (int64, error) {
	if len(records) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	models := make([]mongo.WriteModel, 0, len(records))

	for _, rec := range records {
		filter := bson.M{"agency_id": rec.AgencyID, "policy_no": rec.PolicyNo}
		update := bson.M{
			"$set": bson.M{
				"assured_name":         rec.AssuredName,
				"doc":                  rec.DOC,
				"plan_code":            rec.PlanCode,
				"term":                 rec.Term,
				"mode":                 rec.Mode,
				"fup":                  rec.FUP,
				"flag":                 rec.Flag,
				"installment_premium":  rec.InstallmentPremium,
				"due_count":            rec.DueCount,
				"total_premium":        rec.TotalPremium,
				"estimated_commission": rec.EstimatedCommission,
				"next_due_date":        rec.NextDueDate,
				"agent_code":           rec.AgentCode,
				"due_month":            rec.DueMonth,
				"last_synced_at":       now,
				"updated_at":           now,
			},
			"$setOnInsert": bson.M{
				"agency_id":     rec.AgencyID,
				"policy_no":     rec.PolicyNo,
				"first_seen_at": now,
				"created_at":    now,
			},
		}

		models = append(models, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true))
	}

	result, err := r.collection.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		// A partial failure still tells us how many rows landed, which is what the caller reports.
		if result != nil {
			return result.UpsertedCount, fmt.Errorf("failed to store some imported policies: %w", err)
		}
		return 0, fmt.Errorf("failed to store imported policies: %w", err)
	}

	return result.UpsertedCount, nil
}

// linkStageFor builds the pipeline stages that decide, live, whether an inbox row is already held
// by a client of this agency. Nothing about link state is stored on the row itself, so a policy
// created by hand, deleted, or moved between clients is reflected the moment it happens.
//
// IsLinked deliberately requires the matched policy's owner to be in *this* agency: a policy number
// held by another agency's client reads as unclaimed here and their name is never projected.
func linkStageFor(agencyID string) mongo.Pipeline {
	return mongo.Pipeline{
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: lifeInsurancesCollection},
			{Key: "localField", Value: "policy_no"},
			{Key: "foreignField", Value: "policy_details.policy_no"},
			{Key: "as", Value: "matched_policies"},
		}}},
		bson.D{{Key: "$addFields", Value: bson.D{
			{Key: "matched_policy", Value: bson.D{{Key: "$arrayElemAt", Value: bson.A{"$matched_policies", 0}}}},
		}}},
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: usersCollection},
			{Key: "localField", Value: "matched_policy.user_id"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "matched_owners"},
		}}},
		bson.D{{Key: "$addFields", Value: bson.D{
			{Key: "matched_owner", Value: bson.D{{Key: "$arrayElemAt", Value: bson.A{"$matched_owners", 0}}}},
		}}},
		bson.D{{Key: "$addFields", Value: bson.D{
			{Key: "is_linked", Value: bson.D{{Key: "$eq", Value: bson.A{
				bson.D{{Key: "$ifNull", Value: bson.A{"$matched_owner.agency_id", ""}}},
				agencyID,
			}}}},
		}}},
	}
}

// FindAll returns the agency's policy inbox, filtered by derived link status and an optional
// search across policy number and assured name.
func (r *importedPolicyRepository) FindAll(ctx context.Context, agencyID, status, search string, page, limit int64) ([]*domain.ImportedPolicyView, int64, error) {
	skip := (page - 1) * limit

	basePipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{{Key: "agency_id", Value: agencyID}}}},
	}

	if search != "" {
		// QuoteMeta so a user-supplied search string can never be interpreted as a regex.
		safe := regexp.QuoteMeta(search)
		basePipeline = append(basePipeline, bson.D{{Key: "$match", Value: bson.D{
			{Key: "$or", Value: bson.A{
				bson.D{{Key: "policy_no", Value: bson.D{{Key: "$regex", Value: safe}, {Key: "$options", Value: "i"}}}},
				bson.D{{Key: "assured_name", Value: bson.D{{Key: "$regex", Value: safe}, {Key: "$options", Value: "i"}}}},
			}},
		}}})
	}

	basePipeline = append(basePipeline, linkStageFor(agencyID)...)

	switch status {
	case domain.ImportedPolicyStatusUnclaimed:
		basePipeline = append(basePipeline, bson.D{{Key: "$match", Value: bson.D{{Key: "is_linked", Value: false}}}})
	case domain.ImportedPolicyStatusLinked:
		basePipeline = append(basePipeline, bson.D{{Key: "$match", Value: bson.D{{Key: "is_linked", Value: true}}}})
	}

	countPipeline := append(mongo.Pipeline{}, basePipeline...)
	countPipeline = append(countPipeline, bson.D{{Key: "$count", Value: "total"}})

	countCursor, err := r.collection.Aggregate(ctx, countPipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count imported policies: %w", err)
	}
	defer countCursor.Close(ctx)

	var countResult []struct {
		Total int64 `bson:"total"`
	}
	if err := countCursor.All(ctx, &countResult); err != nil {
		return nil, 0, fmt.Errorf("failed to decode imported policy count: %w", err)
	}
	var total int64
	if len(countResult) > 0 {
		total = countResult[0].Total
	}

	pipeline := append(mongo.Pipeline{}, basePipeline...)
	pipeline = append(pipeline,
		// Soonest due first — that is the order an agent actually works the list in.
		bson.D{{Key: "$sort", Value: bson.D{{Key: "next_due_date", Value: 1}, {Key: "_id", Value: 1}}}},
		bson.D{{Key: "$skip", Value: skip}},
		bson.D{{Key: "$limit", Value: limit}},
		// Only surface the owner when they belong to this agency (is_linked already guarantees that),
		// so a cross-agency holder's name can never be projected. These have to be added and the
		// join scratch fields dropped in separate stages — a single $project cannot both compute
		// new fields and exclude existing ones.
		bson.D{{Key: "$addFields", Value: bson.D{
			{Key: "linked_policy_id", Value: bson.D{{Key: "$cond", Value: bson.A{"$is_linked", "$matched_policy._id", "$$REMOVE"}}}},
			{Key: "linked_user_id", Value: bson.D{{Key: "$cond", Value: bson.A{"$is_linked", "$matched_policy.user_id", "$$REMOVE"}}}},
			{Key: "linked_client", Value: bson.D{{Key: "$cond", Value: bson.A{"$is_linked", "$matched_owner.name", "$$REMOVE"}}}},
		}}},
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "matched_policies", Value: 0},
			{Key: "matched_owners", Value: 0},
			{Key: "matched_policy", Value: 0},
			{Key: "matched_owner", Value: 0},
		}}},
	)

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query imported policies: %w", err)
	}
	defer cursor.Close(ctx)

	var views []*domain.ImportedPolicyView
	if err := cursor.All(ctx, &views); err != nil {
		return nil, 0, fmt.Errorf("failed to decode imported policies: %w", err)
	}

	if views == nil {
		views = []*domain.ImportedPolicyView{}
	}

	return views, total, nil
}

// FindByID retrieves a single inbox row. Agency ownership is checked by the service layer.
func (r *importedPolicyRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.ImportedPolicy, error) {
	var record domain.ImportedPolicy
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&record)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("imported policy not found")
		}
		return nil, fmt.Errorf("failed to find imported policy: %w", err)
	}
	return &record, nil
}

// Delete removes an inbox row by ID.
func (r *importedPolicyRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete imported policy: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("imported policy not found")
	}
	return nil
}

// CountByStatus returns how many of the agency's inbox rows are still unclaimed vs already linked.
func (r *importedPolicyRepository) CountByStatus(ctx context.Context, agencyID string) (int64, int64, error) {
	pipeline := append(mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{{Key: "agency_id", Value: agencyID}}}},
	}, linkStageFor(agencyID)...)
	pipeline = append(pipeline, bson.D{{Key: "$group", Value: bson.D{
		{Key: "_id", Value: "$is_linked"},
		{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
	}}})

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count imported policies by status: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		IsLinked bool  `bson:"_id"`
		Count    int64 `bson:"count"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return 0, 0, fmt.Errorf("failed to decode imported policy counts: %w", err)
	}

	var unclaimed, linked int64
	for _, row := range rows {
		if row.IsLinked {
			linked = row.Count
		} else {
			unclaimed = row.Count
		}
	}

	return unclaimed, linked, nil
}
