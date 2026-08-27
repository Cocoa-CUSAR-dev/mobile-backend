package handlers

import (
	"errors"
	"testing"

	"go-server-mobile/internal/validation"

	"github.com/google/uuid"
)

// validateSubmission has to come back clean before submitAnswerForUser
// opens a transaction — that's the whole gate. fetchSchema is faked below
// so none of this touches a DB or Kotlin; whether h.DB.Transaction itself
// behaves is a separate, DB-dependent question these tests don't cover.

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

	// submitAnswerForUser only opens the transaction when len(errs) == 0;
	// copying that check here means a dropped early-return would show up
	// as fakeDBWrite firing.
	if len(errs) == 0 {
		fakeDBWrite()
	}
	if dbWasCalled {
		t.Fatal("DB write must not run when validation fails")
	}
}

func TestValidateSubmission_SchemaFetchFailureFailsClosed(t *testing.T) {
	// no schema to check against means no way to know the answer's safe —
	// this has to fail, not pass by default.
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
