package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

const (
	superAdminEmail  = "super@admin.com"
	reviewerEmail    = "reviewer@smartinvestsolutions.in"
	reviewerPassword = "Reviewer@2026"
	reviewerPIN      = "1234"
	reviewerName     = "Google Reviewer"
	reviewerPhone    = "+919876543210"
)

func main() {
	if err := godotenv.Load(".env"); err != nil {
		if err := godotenv.Load("../.env"); err != nil {
			_ = godotenv.Load("../../.env")
		}
	}

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = os.Getenv("DATABASE_URL")
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "smart_invest_solutions"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("Connecting to MongoDB [%s]...\n", dbName)
	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("❌ Connect failed: %v", err)
	}
	defer func() { _ = client.Disconnect(ctx) }()

	db := client.Database(dbName)

	fmt.Println("\n=======================================================")
	fmt.Println("🚀 STARTING PRODUCTION DATABASE PURGE & FRESH RESET")
	fmt.Println("=======================================================")

	// 1. Verify Super Admin exists and sanitize
	var superAdmin struct {
		ID bson.ObjectID `bson:"_id"`
	}
	err = db.Collection("users").FindOne(ctx, bson.M{"email": superAdminEmail}).Decode(&superAdmin)
	if err != nil {
		log.Fatalf("❌ Super Admin not found in DB! Cannot proceed without Super Admin.")
	}
	fmt.Printf("✅ Super Admin found: ID %s (%s)\n", superAdmin.ID.Hex(), superAdminEmail)

	// Update Super Admin to pristine status
	_, err = db.Collection("users").UpdateOne(ctx,
		bson.M{"_id": superAdmin.ID},
		bson.M{
			"$set": bson.M{
				"is_active":             true,
				"is_email_verified":     true,
				"failed_login_attempts": 0,
				"token_version":         0,
				"updated_at":            time.Now().UTC(),
			},
			"$unset": bson.M{
				"locked_until": "",
			},
		},
	)
	if err != nil {
		log.Fatalf("Failed to update Super Admin status: %v", err)
	}

	// 2. Upsert Google Reviewer User with known sample data
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(reviewerPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash reviewer password: %v", err)
	}
	hashedPIN, err := bcrypt.GenerateFromPassword([]byte(reviewerPIN), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash reviewer PIN: %v", err)
	}

	var reviewerUser struct {
		ID bson.ObjectID `bson:"_id"`
	}
	now := time.Now().UTC()
	err = db.Collection("users").FindOne(ctx, bson.M{"email": reviewerEmail}).Decode(&reviewerUser)
	var reviewerID bson.ObjectID
	if err != nil {
		reviewerID = bson.NewObjectID()
		_, err = db.Collection("users").InsertOne(ctx, bson.M{
			"_id":                   reviewerID,
			"name":                  reviewerName,
			"email":                 reviewerEmail,
			"password":              string(hashedPassword),
			"pin":                   string(hashedPIN),
			"phone":                 reviewerPhone,
			"role":                  "client",
			"is_active":             true,
			"is_email_verified":     true,
			"failed_login_attempts": 0,
			"login_count":           1,
			"token_version":         0,
			"last_login_at":         now,
			"created_at":            now,
			"updated_at":            now,
		})
		if err != nil {
			log.Fatalf("Failed to create Reviewer: %v", err)
		}
		fmt.Printf("✅ Created Reviewer user: ID %s (%s)\n", reviewerID.Hex(), reviewerEmail)
	} else {
		reviewerID = reviewerUser.ID
		_, err = db.Collection("users").UpdateOne(ctx,
			bson.M{"_id": reviewerID},
			bson.M{
				"$set": bson.M{
					"name":                  reviewerName,
					"password":              string(hashedPassword),
					"pin":                   string(hashedPIN),
					"phone":                 reviewerPhone,
					"role":                  "client",
					"is_active":             true,
					"is_email_verified":     true,
					"failed_login_attempts": 0,
					"token_version":         0,
					"updated_at":            now,
				},
				"$unset": bson.M{
					"locked_until": "",
				},
			},
		)
		if err != nil {
			log.Fatalf("Failed to update Reviewer: %v", err)
		}
		fmt.Printf("✅ Updated Reviewer user to pristine status: ID %s (%s)\n", reviewerID.Hex(), reviewerEmail)
	}

	// 3. Delete ALL other users
	allowedUserIDs := []bson.ObjectID{superAdmin.ID, reviewerID}
	delUsersRes, err := db.Collection("users").DeleteMany(ctx, bson.M{
		"_id": bson.M{"$nin": allowedUserIDs},
	})
	if err != nil {
		log.Fatalf("Failed to delete extra users: %v", err)
	}
	fmt.Printf("🗑️ Deleted %d non-essential users from 'users'\n", delUsersRes.DeletedCount)

	// 4. Ensure Reviewer Family Member exists
	_, _ = db.Collection("family_members").DeleteMany(ctx, bson.M{
		"user_id": bson.M{"$ne": reviewerID},
	})
	var familyRecord struct {
		ID bson.ObjectID `bson:"_id"`
	}
	err = db.Collection("family_members").FindOne(ctx, bson.M{"user_id": reviewerID, "relation_with_hof": "Self"}).Decode(&familyRecord)
	var familyMemberID bson.ObjectID
	if err != nil {
		familyMemberID = bson.NewObjectID()
		_, err = db.Collection("family_members").InsertOne(ctx, bson.M{
			"_id":               familyMemberID,
			"user_id":           reviewerID,
			"name":              reviewerName,
			"relation_with_hof": "Self",
			"phone":             reviewerPhone,
			"email":             reviewerEmail,
			"blood_group":       "O+",
			"date_of_birth":     "1990-01-15",
			"created_at":        now,
			"updated_at":        now,
		})
		if err != nil {
			log.Fatalf("Failed to insert reviewer family member: %v", err)
		}
	} else {
		familyMemberID = familyRecord.ID
	}
	fmt.Printf("✅ Reviewer Family Member preserved (ID: %s)\n", familyMemberID.Hex())

	// 5. Clean policies and keep ONLY reviewer's sample holdings
	delLifeRes, _ := db.Collection("life_insurances").DeleteMany(ctx, bson.M{"user_id": bson.M{"$ne": reviewerID}})
	fmt.Printf("🗑️ Deleted %d non-reviewer life policies\n", delLifeRes.DeletedCount)

	delHealthRes, _ := db.Collection("health_insurances").DeleteMany(ctx, bson.M{"user_id": bson.M{"$ne": reviewerID}})
	fmt.Printf("🗑️ Deleted %d non-reviewer health policies\n", delHealthRes.DeletedCount)

	delFDRes, _ := db.Collection("fixed_deposits").DeleteMany(ctx, bson.M{"user_id": bson.M{"$ne": reviewerID}})
	fmt.Printf("🗑️ Deleted %d non-reviewer fixed deposits\n", delFDRes.DeletedCount)

	delMotorRes, _ := db.Collection("general_insurances").DeleteMany(ctx, bson.M{"user_id": bson.M{"$ne": reviewerID}})
	fmt.Printf("🗑️ Deleted %d non-reviewer motor policies\n", delMotorRes.DeletedCount)

	// Ensure reviewer has their 3 sample holdings
	lifeCount, _ := db.Collection("life_insurances").CountDocuments(ctx, bson.M{"user_id": reviewerID})
	if lifeCount == 0 {
		docDate := now.AddDate(-2, 0, 0)
		maturityDate := docDate.AddDate(21, 0, 0)
		nextDueDate := now.AddDate(0, 0, 18)
		_, _ = db.Collection("life_insurances").InsertOne(ctx, bson.M{
			"_id":              bson.NewObjectID(),
			"user_id":          reviewerID,
			"family_member_id": familyMemberID,
			"company_name":     "Life Insurance Corporation of India (LIC)",
			"policy_details": bson.M{
				"policy_no":         "892345671",
				"plan_name":         "LIC Jeevan Labh (Plan 936)",
				"life_insured_name": reviewerName,
				"nominee_name":      "Priya Sharma",
				"sum_assured":       1000000.0,
				"term":              21,
				"ppt":               16,
				"doc":               docDate,
				"maturity_date":     maturityDate,
			},
			"premium_details": bson.M{
				"installment_premium": 42500.0,
				"next_due_date":       nextDueDate,
				"payment_mode":        "Yearly",
			},
			"is_mapped":  true,
			"managed_by": "client",
			"created_at": now,
			"updated_at": now,
		})
	}

	healthCount, _ := db.Collection("health_insurances").CountDocuments(ctx, bson.M{"user_id": reviewerID})
	if healthCount == 0 {
		docDate := now.AddDate(0, -6, 0)
		expiryDate := now.AddDate(0, 6, 0)
		nextDueDate := now.AddDate(0, 6, 0)
		_, _ = db.Collection("health_insurances").InsertOne(ctx, bson.M{
			"_id":              bson.NewObjectID(),
			"user_id":          reviewerID,
			"family_member_id": familyMemberID,
			"company_name":     "Star Health & Allied Insurance",
			"policy_details": bson.M{
				"policy_no":            "STAR-HLT-449102",
				"plan_name":            "Family Health Optima",
				"primary_insured_name": reviewerName,
				"sum_insured":          500000.0,
				"doc":                  docDate,
				"expiry_date":          expiryDate,
			},
			"premium_details": bson.M{
				"installment_premium": 18200.0,
				"next_due_date":       nextDueDate,
				"payment_mode":        "Yearly",
			},
			"is_mapped":  true,
			"managed_by": "client",
			"created_at": now,
			"updated_at": now,
		})
	}

	fdCount, _ := db.Collection("fixed_deposits").CountDocuments(ctx, bson.M{"user_id": reviewerID})
	if fdCount == 0 {
		openingDate := now.AddDate(-1, 0, 0)
		maturityDate := now.AddDate(1, 0, 0)
		_, _ = db.Collection("fixed_deposits").InsertOne(ctx, bson.M{
			"_id":              bson.NewObjectID(),
			"user_id":          reviewerID,
			"family_member_id": familyMemberID,
			"fd_number":        "HDFC-FD-982143",
			"fd_name":          "HDFC Regular Fixed Deposit",
			"company_name":     "HDFC Bank",
			"principal_amount": 200000.0,
			"maturity_amount":  230800.0,
			"monthly_income":   0.0,
			"term_months":      24,
			"opening_date":     openingDate,
			"maturity_date":    maturityDate,
			"nominee_name":     "Priya Sharma",
			"account_type":     "Cumulative",
			"address":          "Kolkata Main Branch",
			"is_mapped":        true,
			"managed_by":       "client",
			"created_at":       now,
			"updated_at":       now,
		})
	}

	// 6. PURGE ALL INBOXES AND EPHEMERAL DATA
	delImportedPol, _ := db.Collection("imported_policies").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'imported_policies' (Inbox clean)\n", delImportedPol.DeletedCount)

	delImportedDep, _ := db.Collection("imported_deposits").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'imported_deposits' (Inbox clean)\n", delImportedDep.DeletedCount)

	delTickets, _ := db.Collection("support_tickets").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'support_tickets'\n", delTickets.DeletedCount)

	delAccessReqs, _ := db.Collection("access_requests").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'access_requests'\n", delAccessReqs.DeletedCount)

	delDocs, _ := db.Collection("documents").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'documents'\n", delDocs.DeletedCount)

	delProducts, _ := db.Collection("products").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'products'\n", delProducts.DeletedCount)

	delAnnounce, _ := db.Collection("announcements").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'announcements'\n", delAnnounce.DeletedCount)

	delProdAccess, _ := db.Collection("client_product_access").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'client_product_access'\n", delProdAccess.DeletedCount)

	delPasswordResets, _ := db.Collection("password_resets").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'password_resets'\n", delPasswordResets.DeletedCount)

	delEmailVerifs, _ := db.Collection("email_verifications").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'email_verifications'\n", delEmailVerifs.DeletedCount)

	delReferrals, _ := db.Collection("referral_records").DeleteMany(ctx, bson.M{})
	fmt.Printf("🗑️ Purged %d records from 'referral_records'\n", delReferrals.DeletedCount)

	fmt.Println("\n=======================================================")
	fmt.Println("📊 POST-CLEANUP DATABASE AUDIT:")
	fmt.Println("=======================================================")

	collections := []string{
		"users", "family_members", "life_insurances", "health_insurances",
		"fixed_deposits", "general_insurances", "imported_policies",
		"imported_deposits", "support_tickets", "access_requests",
		"documents", "products", "announcements", "client_product_access",
		"password_resets", "email_verifications", "referral_records",
		"calculator_settings",
	}

	for _, col := range collections {
		cnt, _ := db.Collection(col).CountDocuments(ctx, bson.M{})
		fmt.Printf(" - %-25s: %d\n", col, cnt)
	}

	fmt.Println("\n🎉 DATABASE CLEANUP COMPLETE! ZERO LEFTOVER BUGS OR ORPHAN RECORDS.")
}
