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

const fixedDepositsCollection = "fixed_deposits"

type importedDepositRepository struct {
	collection *mongo.Collection
}

// NewImportedDepositRepository initializes the imported-deposit inbox repository.
func NewImportedDepositRepository(db *mongo.Database) domain.ImportedDepositRepository {
	col := db.Collection("imported_deposits")

	// Identity of an imported row: one account number per agency.
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "agency_id", Value: 1}, {Key: "account_no", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	// The inbox lists soonest-maturing first within an agency.
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "agency_id", Value: 1}, {Key: "maturity_date", Value: 1}},
	})

	return &importedDepositRepository{collection: col}
}

// BulkUpsertFromSync writes one row per account for the agency, refreshing report-sourced fields on
// rows that already exist. Only the first upload of an account counts as newly imported.
func (r *importedDepositRepository) BulkUpsertFromSync(ctx context.Context, records []*domain.ImportedDeposit) (int64, error) {
	if len(records) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	models := make([]mongo.WriteModel, 0, len(records))

	for _, rec := range records {
		filter := bson.M{"agency_id": rec.AgencyID, "account_no": rec.AccountNo}
		update := bson.M{
			"$set": bson.M{
				"holder_name":       rec.HolderName,
				"joint_holder_name": rec.JointHolderName,
				"scheme":            rec.Scheme,
				"scheme_code":       rec.SchemeCode,
				"term_months":       rec.TermMonths,
				"deposit_amount":    rec.DepositAmount,
				"maturity_amount":   rec.MaturityAmount,
				"monthly_income":    rec.MonthlyIncome,
				"issue_date":        rec.IssueDate,
				"maturity_date":     rec.MaturityDate,
				"remarks":           rec.Remarks,
				"report_name":       rec.ReportName,
				"last_synced_at":    now,
				"updated_at":        now,
			},
			"$setOnInsert": bson.M{
				"agency_id":     rec.AgencyID,
				"account_no":    rec.AccountNo,
				"first_seen_at": now,
				"created_at":    now,
			},
		}

		models = append(models, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true))
	}

	result, err := r.collection.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		if result != nil {
			return result.UpsertedCount, fmt.Errorf("failed to store some imported deposits: %w", err)
		}
		return 0, fmt.Errorf("failed to store imported deposits: %w", err)
	}

	return result.UpsertedCount, nil
}

// depositLinkStageFor resolves, per imported row, whether a client of this agency already holds the
// account — matched on the deposit's FD number, with the owner's agency checked so one agency never
// sees another's client.
func depositLinkStageFor(agencyID string) mongo.Pipeline {
	return mongo.Pipeline{
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: fixedDepositsCollection},
			{Key: "localField", Value: "account_no"},
			{Key: "foreignField", Value: "fd_number"},
			{Key: "as", Value: "matched_deposits"},
		}}},
		bson.D{{Key: "$addFields", Value: bson.D{
			{Key: "matched_deposit", Value: bson.D{{Key: "$arrayElemAt", Value: bson.A{"$matched_deposits", 0}}}},
		}}},
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: usersCollection},
			{Key: "localField", Value: "matched_deposit.user_id"},
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

// FindAll returns the agency's deposit inbox, soonest maturity first.
func (r *importedDepositRepository) FindAll(ctx context.Context, agencyID, status, search string, page, limit int64) ([]*domain.ImportedDepositView, int64, error) {
	skip := (page - 1) * limit

	basePipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{{Key: "agency_id", Value: agencyID}}}},
	}

	if search != "" {
		safe := regexp.QuoteMeta(search)
		basePipeline = append(basePipeline, bson.D{{Key: "$match", Value: bson.D{
			{Key: "$or", Value: bson.A{
				bson.D{{Key: "account_no", Value: bson.D{{Key: "$regex", Value: safe}, {Key: "$options", Value: "i"}}}},
				bson.D{{Key: "holder_name", Value: bson.D{{Key: "$regex", Value: safe}, {Key: "$options", Value: "i"}}}},
				bson.D{{Key: "joint_holder_name", Value: bson.D{{Key: "$regex", Value: safe}, {Key: "$options", Value: "i"}}}},
			}},
		}}})
	}

	basePipeline = append(basePipeline, depositLinkStageFor(agencyID)...)

	switch status {
	case domain.ImportedDepositStatusUnclaimed:
		basePipeline = append(basePipeline, bson.D{{Key: "$match", Value: bson.D{{Key: "is_linked", Value: false}}}})
	case domain.ImportedDepositStatusLinked:
		basePipeline = append(basePipeline, bson.D{{Key: "$match", Value: bson.D{{Key: "is_linked", Value: true}}}})
	}

	countPipeline := append(mongo.Pipeline{}, basePipeline...)
	countPipeline = append(countPipeline, bson.D{{Key: "$count", Value: "total"}})

	countCursor, err := r.collection.Aggregate(ctx, countPipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count imported deposits: %w", err)
	}
	defer countCursor.Close(ctx)

	var countResult []struct {
		Total int64 `bson:"total"`
	}
	if err := countCursor.All(ctx, &countResult); err != nil {
		return nil, 0, fmt.Errorf("failed to decode imported deposit count: %w", err)
	}
	var total int64
	if len(countResult) > 0 {
		total = countResult[0].Total
	}

	pipeline := append(mongo.Pipeline{}, basePipeline...)
	pipeline = append(pipeline,
		bson.D{{Key: "$sort", Value: bson.D{{Key: "maturity_date", Value: 1}, {Key: "_id", Value: 1}}}},
		bson.D{{Key: "$skip", Value: skip}},
		bson.D{{Key: "$limit", Value: limit}},
		bson.D{{Key: "$addFields", Value: bson.D{
			{Key: "linked_deposit_id", Value: bson.D{{Key: "$cond", Value: bson.A{"$is_linked", "$matched_deposit._id", "$$REMOVE"}}}},
			{Key: "linked_user_id", Value: bson.D{{Key: "$cond", Value: bson.A{"$is_linked", "$matched_deposit.user_id", "$$REMOVE"}}}},
			{Key: "linked_client", Value: bson.D{{Key: "$cond", Value: bson.A{"$is_linked", "$matched_owner.name", "$$REMOVE"}}}},
		}}},
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "matched_deposits", Value: 0},
			{Key: "matched_owners", Value: 0},
			{Key: "matched_deposit", Value: 0},
			{Key: "matched_owner", Value: 0},
		}}},
	)

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query imported deposits: %w", err)
	}
	defer cursor.Close(ctx)

	var views []*domain.ImportedDepositView
	if err := cursor.All(ctx, &views); err != nil {
		return nil, 0, fmt.Errorf("failed to decode imported deposits: %w", err)
	}

	if views == nil {
		views = []*domain.ImportedDepositView{}
	}

	return views, total, nil
}

// FindByID retrieves a single imported deposit.
func (r *importedDepositRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.ImportedDeposit, error) {
	var record domain.ImportedDeposit
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&record)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("imported deposit not found")
		}
		return nil, fmt.Errorf("failed to find imported deposit: %w", err)
	}
	return &record, nil
}

// Delete removes a row from the inbox.
func (r *importedDepositRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete imported deposit: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("imported deposit not found")
	}
	return nil
}

// CountByStatus returns the agency's (unclaimed, linked) split.
func (r *importedDepositRepository) CountByStatus(ctx context.Context, agencyID string) (int64, int64, error) {
	pipeline := append(mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{{Key: "agency_id", Value: agencyID}}}},
	}, depositLinkStageFor(agencyID)...)
	pipeline = append(pipeline, bson.D{{Key: "$group", Value: bson.D{
		{Key: "_id", Value: "$is_linked"},
		{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
	}}})

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count imported deposits by status: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []struct {
		IsLinked bool  `bson:"_id"`
		Count    int64 `bson:"count"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return 0, 0, fmt.Errorf("failed to decode imported deposit counts: %w", err)
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
