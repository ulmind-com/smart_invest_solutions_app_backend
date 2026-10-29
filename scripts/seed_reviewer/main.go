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
	reviewerEmail    = "reviewer@smartinvestsolutions.in"
	reviewerPassword = "Reviewer@2026"
	reviewerPIN      = "1234"
	reviewerName     = "Google Reviewer"
	reviewerPhone    = "+919876543210"
)

func main() {
	// 1. Load .env
	if err := godotenv.Load(".env"); err != nil {
		if err := godotenv.Load("../.env"); err != nil {
			log.Println("Note: no .env file loaded, checking environment variables")
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

	if mongoURI == "" {
		log.Fatal("❌ Error: MONGODB_URI not found in environment")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	fmt.Printf("Connecting to MongoDB Database [%s]...\n", dbName)
	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("❌ Failed to connect to MongoDB: %v", err)
	}
	defer func() { _ = client.Disconnect(ctx) }()

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("❌ MongoDB ping failed: %v", err)
	}
	fmt.Println(" Connected to MongoDB Atlas successfully.")

	db := client.Database(dbName)
	usersColl := db.Collection("users")
	familyColl := db.Collection("family_members")
	lifeColl := db.Collection("life_insurances")
	healthColl := db.Collection("health_insurances")
	fdColl := db.Collection("fixed_deposits")

	// 2. Hash Password and PIN
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(reviewerPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}
	hashedPIN, err := bcrypt.GenerateFromPassword([]byte(reviewerPIN), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash PIN: %v", err)
	}

	now := time.Now().UTC()

	// 3. Find or create Reviewer User
	var userRecord struct {
		ID bson.ObjectID `bson:"_id"`
	}

	filter := bson.M{"email": reviewerEmail}
	err = usersColl.FindOne(ctx, filter).Decode(&userRecord)

	var userID bson.ObjectID
	if err != nil {
		// Create new user
		userID = bson.NewObjectID()
		newUser := bson.M{
			"_id":                   userID,
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
			"last_login_at":         now,
			"created_at":            now,
			"updated_at":            now,
		}
		_, err = usersColl.InsertOne(ctx, newUser)
		if err != nil {
			log.Fatalf("Failed to insert user: %v", err)
		}
		fmt.Printf(" Created active reviewer user (ID: %s)\n", userID.Hex())
	} else {
		userID = userRecord.ID
		update := bson.M{
			"$set": bson.M{
				"name":                  reviewerName,
				"password":              string(hashedPassword),
				"pin":                   string(hashedPIN),
				"phone":                 reviewerPhone,
				"role":                  "client",
				"is_active":             true,
				"is_email_verified":     true,
				"failed_login_attempts": 0,
				"locked_until":          nil,
				"updated_at":            now,
			},
		}
		_, err = usersColl.UpdateOne(ctx, filter, update)
		if err != nil {
			log.Fatalf("Failed to update user: %v", err)
		}
		fmt.Printf(" Existing reviewer user updated to active status (ID: %s)\n", userID.Hex())
	}

	// 4. Upsert Family Member (Self / Primary)
	var memberRecord struct {
		ID bson.ObjectID `bson:"_id"`
	}
	err = familyColl.FindOne(ctx, bson.M{"user_id": userID, "relation_with_hof": "Self"}).Decode(&memberRecord)
	var familyMemberID bson.ObjectID
	if err != nil {
		familyMemberID = bson.NewObjectID()
		newMember := bson.M{
			"_id":               familyMemberID,
			"user_id":           userID,
			"name":              reviewerName,
			"relation_with_hof": "Self",
			"phone":             reviewerPhone,
			"email":             reviewerEmail,
			"blood_group":       "O+",
			"date_of_birth":     "1990-01-15",
			"created_at":        now,
			"updated_at":        now,
		}
		_, err = familyColl.InsertOne(ctx, newMember)
		if err != nil {
			log.Fatalf("Failed to insert family member: %v", err)
		}
		fmt.Printf(" Added family member Self (ID: %s)\n", familyMemberID.Hex())
	} else {
		familyMemberID = memberRecord.ID
	}

	// 5. Upsert Life Insurance Sample Policy
	lifeCount, _ := lifeColl.CountDocuments(ctx, bson.M{"user_id": userID})
	if lifeCount == 0 {
		docDate := now.AddDate(-2, 0, 0)
		maturityDate := docDate.AddDate(21, 0, 0)
		nextDueDate := now.AddDate(0, 0, 18) // Due in 18 days -> shows up on upcoming premiums!

		newLife := bson.M{
			"_id":              bson.NewObjectID(),
			"user_id":          userID,
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
		}
		_, err = lifeColl.InsertOne(ctx, newLife)
		if err != nil {
			log.Printf("Warning: failed to insert life policy: %v", err)
		} else {
			fmt.Println(" Added sample Life Insurance policy: LIC Jeevan Labh (Sum Assured: ₹10,00,000)")
		}
	}

	// 6. Upsert Health Insurance Sample Policy
	healthCount, _ := healthColl.CountDocuments(ctx, bson.M{"user_id": userID})
	if healthCount == 0 {
		docDate := now.AddDate(0, -6, 0)
		expiryDate := now.AddDate(0, 6, 0)
		nextDueDate := now.AddDate(0, 6, 0)

		newHealth := bson.M{
			"_id":              bson.NewObjectID(),
			"user_id":          userID,
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
		}
		_, err = healthColl.InsertOne(ctx, newHealth)
		if err != nil {
			log.Printf("Warning: failed to insert health policy: %v", err)
		} else {
			fmt.Println(" Added sample Health Insurance policy: Star Health Family Optima (Sum Insured: ₹5,00,000)")
		}
	}

	// 7. Upsert Fixed Deposit Sample Record
	fdCount, _ := fdColl.CountDocuments(ctx, bson.M{"user_id": userID})
	if fdCount == 0 {
		openingDate := now.AddDate(-1, 0, 0)
		maturityDate := now.AddDate(1, 0, 0)

		newFD := bson.M{
			"_id":              bson.NewObjectID(),
			"user_id":          userID,
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
		}
		_, err = fdColl.InsertOne(ctx, newFD)
		if err != nil {
			log.Printf("Warning: failed to insert fixed deposit: %v", err)
		} else {
			fmt.Println(" Added sample Fixed Deposit: HDFC Bank (Principal: ₹2,00,000, Maturity: ₹2,30,800)")
		}
	}

	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🎉 TEST ACCOUNT CREATED & VERIFIED FOR GOOGLE PLAY REVIEW!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("   Email    : %s\n", reviewerEmail)
	fmt.Printf("   Password : %s\n", reviewerPassword)
	fmt.Printf("   PIN      : %s (Direct 4-Digit PIN pad on login)\n", reviewerPIN)
	fmt.Println("   Status   : Active (IsActive=true, IsEmailVerified=true)")
	fmt.Println("   Role     : Client (Full App Access)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

