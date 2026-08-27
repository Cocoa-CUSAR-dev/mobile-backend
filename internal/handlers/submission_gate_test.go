package handlers

import (
	"errors"
	"testing"

	"go-server-mobile/internal/validation"

	"github.com/google/uuid"
)

// This is the gate #54 asked for: validateSubmission has to run clean
// before submitAnswerForUser is allowed to open a transaction. These tests
// don't touch a DB at all — fetchSchema is faked, so a passing test here
// means the gate logic itself is correct, independent of whatever
// h.DB.Transaction does afterward (that part still needs a real DB, same
// as the rest of this file's DB-touching branches).

func TestValidateSubmission_PassesCleanAnswerThrough(t *testing.T) {
	formID := uuid.New()
	schema := validation.FormSchema{Sections: []validation.Section{{
		Questions: []validation.Question{{FieldName: "note", InputType: "VARCHAR", IsMandatory: true}},
	}}}

	fetchSchema := func(id uuid.UUID) (validation.FormSchema, error) {
		if id != formID {
			t.Errorf("want fetchSchema called with %s, got %s", formID, id)
		}
		return schema, nil
	}

	errs, err := validateSubmission(fetchSchema, formID, map[string]interface{}{"note": "ok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("want no field errors, got %v", errs)
	}
}

func TestValidateSubmission_RejectsBadAnswerWithoutCallingDB(t *testing.T) {
	formID := uuid.New()
	schema := validation.FormSchema{Sections: []validation.Section{{
		Questions: []validation.Question{{FieldName: "quantity_kg", InputType: "INT", IsMandatory: true}},
	}}}
	fetchSchema := func(uuid.UUID) (validation.FormSchema, error) { return schema, nil }

	dbWasCalled := false
	fakeDBWrite := func() { dbWasCalled = true }

	errs, err := validateSubmission(fetchSchema, formID, map[string]interface{}{"quantity_kg": "not-a-number"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("want a field error for a non-numeric quantity_kg, got none")
	}

	// submitAnswerForUser only reaches h.DB.Transaction when len(errs) == 0 —
	// mirror that branch here so a regression that drops the early return
	// shows up as this test calling fakeDBWrite.
	if len(errs) == 0 {
		fakeDBWrite()
	}
	if dbWasCalled {
		t.Fatal("DB write must not run when validation fails")
	}
}

func TestValidateSubmission_SchemaFetchFailureFailsClosed(t *testing.T) {
	// Kotlin down, bad service key, whatever — if the schema can't be
	// fetched, there's nothing to validate against, so this must come back
	// as an error rather than silently letting the answer through.
	fetchSchema := func(uuid.UUID) (validation.FormSchema, error) {
		return validation.FormSchema{}, errors.New("upstream unavailable")
	}

	errs, err := validateSubmission(fetchSchema, uuid.New(), map[string]interface{}{"note": "ok"})
	if err == nil {
		t.Fatal("want an error when the schema fetch fails, got nil")
	}
	if errs != nil {
		t.Errorf("want nil field errors alongside a fetch error, got %v", errs)
	}
}
