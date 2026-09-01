package handlers

import (
	"regexp"
	"testing"
	"time"

	"go-server-mobile/internal/models"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Issue #50 ("TEST - Contract test: researcher ID mapping"): per ADR 0002,
// a brand-new LINE farmer is meant to go through the exact same account
// creation as everyone else (Register(), the plain UUID auth.user_account
// PK) rather than a separate ID scheme — the chatbot composes the existing
// public /register and /liff/link endpoints instead of mobile-backend
// growing a bespoke "new LINE farmer" path. So the actual thing that can
// silently break compatibility isn't a missing ID generator (there isn't
// one to write) — it's Register()'s created user_id failing to survive,
// unchanged, into LinkLineAccount()'s auth.line_identity insert.
//
// These tests exercise the real GORM statement-building against a mocked
// driver connection (sqlmock) — no live Postgres, but the actual SQL and
// parameter binding this repo would send to one.

func newMockGormDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { mockDB.Close() })

	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: mockDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return gdb, mock
}

func TestContract_RegisterNeverSendsAnExplicitUserID(t *testing.T) {
	// If Register() ever started sending user_id explicitly (e.g. a
	// zero-value uuid.UUID{} from a refactor that forgets to omit it),
	// every new account would collide on the all-zeros UUID instead of
	// getting Postgres's gen_random_uuid() -- silently breaking every
	// account created from that point on, LINE or otherwise.
	gdb, mock := newMockGormDB(t)

	genID := uuid.New()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		`INSERT INTO "auth"."user_account" ("username","password_hash","is_password_reset","is_requires_password_reset") VALUES ($1,$2,$3,$4) RETURNING "user_id","created_at","updated_at"`,
	)).WillReturnRows(sqlmock.NewRows([]string{"user_id", "created_at", "updated_at"}).
		AddRow(genID, time.Now(), time.Now()))
	mock.ExpectCommit()

	hash := "bcrypt-hash-stand-in"
	newUser := models.UserAccount{Username: "0812345678", PasswordHash: &hash}
	if err := gdb.Create(&newUser).Error; err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if newUser.UserID != genID {
		t.Fatalf("want UserID populated from the DB-generated value %s, got %s", genID, newUser.UserID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		// A mismatch here means the actual INSERT column list included
		// user_id (or otherwise changed shape) -- the ExpectQuery regex
		// above encodes the current, correct column list.
		t.Fatalf("unmet expectations (INSERT shape changed?): %v", err)
	}
}

func TestContract_RegisteredUserIDSurvivesIntoLineIdentityLink(t *testing.T) {
	// The actual "ID mapping" contract: whatever UUID Register() handed
	// back has to reach auth.line_identity byte-for-byte when
	// LinkLineAccount does linkReq.UserID = user.UserID. This would catch
	// e.g. a future change that re-parses/re-formats the ID through a
	// lossy string conversion somewhere in between.
	gdb, mock := newMockGormDB(t)

	registeredID := uuid.New() // stands in for Register()'s returned user_id

	mock.ExpectBegin()
	// LineLinkRequest (unlike the separate LineIdentity model) declares no
	// primary key field, so GORM has nothing to RETURNING here -- a plain
	// Exec, not a Query. That gap is already tracked separately (PR #14
	// review, "duplicate model" finding); this test just reflects reality.
	mock.ExpectExec(regexp.QuoteMeta(
		`INSERT INTO "auth"."line_identity" ("user_id","line_user_id","display_name") VALUES ($1,$2,$3)`,
	)).WithArgs(registeredID, "Uabc123", "Somchai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Mirrors LinkLineAccount's own construction exactly (auth_handler.go):
	//   linkReq.UserID = user.UserID
	//   linkReq.LineUserID = lineUserID
	//   linkReq.DisplayName = lineName
	var linkReq models.LineLinkRequest
	linkReq.UserID = registeredID
	linkReq.LineUserID = "Uabc123"
	linkReq.DisplayName = "Somchai"

	if err := gdb.Create(&linkReq).Error; err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		// WithArgs(registeredID, ...) above is the assertion that matters:
		// if the bound user_id parameter didn't match registeredID exactly,
		// sqlmock reports it here as an unmet expectation.
		t.Fatalf("registered user_id did not reach the line_identity insert unchanged: %v", err)
	}
}
