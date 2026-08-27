package validation

import (
	"encoding/json"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func fieldNames(errs []FieldError) []string {
	names := make([]string, len(errs))
	for i, e := range errs {
		names[i] = e.FieldName
	}
	return names
}

func hasField(errs []FieldError, name string) bool {
	for _, e := range errs {
		if e.FieldName == name {
			return true
		}
	}
	return false
}

func TestValidateAnswer_AllValid(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{
			{FieldName: "note", InputType: "VARCHAR", IsMandatory: true},
			{FieldName: "quantity_kg", InputType: "INT", IsMandatory: true},
			{FieldName: "weight", InputType: "FLOAT", IsMandatory: false},
			{FieldName: "harvest_date", InputType: "DATE", IsMandatory: true},
			{FieldName: "logged_at", InputType: "DATETIME", IsMandatory: false},
			{FieldName: "is_organic", InputType: "BOOLEAN", IsMandatory: true},
			{FieldName: "grade", InputType: "OPTION", IsMandatory: true, Choices: []Choice{
				{ID: "A", Name: "Grade A"}, {ID: "B", Name: "Grade B"},
			}},
		},
	}}}

	answer := map[string]interface{}{
		"note":         "เก็บเกี่ยวเสร็จแล้ว",
		"quantity_kg":  float64(42),
		"weight":       "12.5",
		"harvest_date": "2026-01-05",
		"logged_at":    "2026-01-05T10:00:00Z",
		"is_organic":   true,
		"grade":        "A",
	}

	if errs := ValidateAnswer(schema, answer); len(errs) != 0 {
		t.Fatalf("want no errors, got %v", errs)
	}
}

func TestValidateAnswer_MissingMandatoryField(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{FieldName: "harvest_date", InputType: "DATE", IsMandatory: true}},
	}}}

	errs := ValidateAnswer(schema, map[string]interface{}{})
	if len(errs) != 1 || errs[0].FieldName != "harvest_date" {
		t.Fatalf("want one error on harvest_date, got %v", errs)
	}
}

func TestValidateAnswer_OptionalFieldAbsentIsFine(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{FieldName: "note", InputType: "VARCHAR", IsMandatory: false}},
	}}}

	if errs := ValidateAnswer(schema, map[string]interface{}{}); len(errs) != 0 {
		t.Fatalf("want no errors for absent optional field, got %v", errs)
	}
}

func TestValidateAnswer_EmptyStringTreatedAsMissing(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{FieldName: "note", InputType: "VARCHAR", IsMandatory: true}},
	}}}

	errs := ValidateAnswer(schema, map[string]interface{}{"note": "   "})
	if len(errs) != 1 {
		t.Fatalf("want whitespace-only string to fail mandatory check, got %v", errs)
	}
}

func TestValidateAnswer_WrongTypes(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{
			{FieldName: "quantity_kg", InputType: "INT", IsMandatory: true},
			{FieldName: "weight", InputType: "FLOAT", IsMandatory: true},
			{FieldName: "harvest_date", InputType: "DATE", IsMandatory: true},
			{FieldName: "is_organic", InputType: "BOOLEAN", IsMandatory: true},
		},
	}}}

	answer := map[string]interface{}{
		"quantity_kg":  "not-a-number",
		"weight":       "also-not-a-number",
		"harvest_date": "05/01/2026", // wrong format
		"is_organic":   "maybe",
	}

	errs := ValidateAnswer(schema, answer)
	if len(errs) != 4 {
		t.Fatalf("want 4 type errors, got %d: %v", len(errs), errs)
	}
	for _, name := range []string{"quantity_kg", "weight", "harvest_date", "is_organic"} {
		if !hasField(errs, name) {
			t.Errorf("expected an error on %q, got %v", name, fieldNames(errs))
		}
	}
}

func TestValidateAnswer_IntRejectsFractional(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{FieldName: "quantity_kg", InputType: "INT", IsMandatory: true}},
	}}}

	errs := ValidateAnswer(schema, map[string]interface{}{"quantity_kg": float64(4.5)})
	if len(errs) != 1 {
		t.Fatalf("want fractional value rejected for INT, got %v", errs)
	}
}

func TestValidateAnswer_OptionRejectsUnknownChoice(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{
			FieldName: "grade", InputType: "OPTION", IsMandatory: true,
			Choices: []Choice{{ID: "A", Name: "Grade A"}},
		}},
	}}}

	errs := ValidateAnswer(schema, map[string]interface{}{"grade": "Z"})
	if len(errs) != 1 || errs[0].FieldName != "grade" {
		t.Fatalf("want grade rejected as unknown choice, got %v", errs)
	}
}

func TestValidateAnswer_OptionAcceptsNumericIDAsString(t *testing.T) {
	// This is the case choiceExists' string-comparison trick is actually
	// for: schema says the ID is 5 (float64, from JSON), answer says "5".
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{
			FieldName: "grade", InputType: "OPTION", IsMandatory: true,
			Choices: []Choice{{ID: float64(5), Name: "Grade 5"}},
		}},
	}}}

	if errs := ValidateAnswer(schema, map[string]interface{}{"grade": "5"}); len(errs) != 0 {
		t.Fatalf("want numeric choice id to match its string form, got %v", errs)
	}
}

func TestValidateAnswer_GeodataRequiresLatLngPoints(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{FieldName: "plot_boundary", InputType: "GEODATA", IsMandatory: true}},
	}}}

	valid := []interface{}{
		map[string]interface{}{"lat": 13.75, "lng": 100.5},
	}
	if errs := ValidateAnswer(schema, map[string]interface{}{"plot_boundary": valid}); len(errs) != 0 {
		t.Fatalf("want valid geodata to pass, got %v", errs)
	}

	invalid := []interface{}{
		map[string]interface{}{"lat": "not-a-number", "lng": 100.5},
	}
	if errs := ValidateAnswer(schema, map[string]interface{}{"plot_boundary": invalid}); len(errs) != 1 {
		t.Fatalf("want invalid geodata point rejected, got %v", errs)
	}

	if errs := ValidateAnswer(schema, map[string]interface{}{"plot_boundary": []interface{}{}}); len(errs) != 1 {
		t.Fatalf("want empty geodata list rejected as missing mandatory field, got %v", errs)
	}
}

func TestValidateAnswer_SkipsInactiveQuestion(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{
			{FieldName: "hidden_field", InputType: "VARCHAR", IsMandatory: true, IsActive: boolPtr(false)},
		},
	}}}

	if errs := ValidateAnswer(schema, map[string]interface{}{}); len(errs) != 0 {
		t.Fatalf("want inactive question skipped even though mandatory, got %v", errs)
	}
}

func TestValidateAnswer_SkipsInactiveSection(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		IsActive: boolPtr(false),
		Questions: []Question{
			{FieldName: "field_in_hidden_section", InputType: "VARCHAR", IsMandatory: true},
		},
	}}}

	if errs := ValidateAnswer(schema, map[string]interface{}{}); len(errs) != 0 {
		t.Fatalf("want questions in an inactive section skipped, got %v", errs)
	}
}

func TestValidateAnswer_MissingIsActiveDefaultsToActive(t *testing.T) {
	// Mirrors mobile-app's `isActive != false` check: absent means shown,
	// only an explicit false hides it.
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{FieldName: "note", InputType: "VARCHAR", IsMandatory: true}},
	}}}

	errs := ValidateAnswer(schema, map[string]interface{}{})
	if len(errs) != 1 {
		t.Fatalf("want question with nil IsActive treated as active, got %v", errs)
	}
}

func TestValidateAnswer_QuestionWithoutFieldNameIsSkipped(t *testing.T) {
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{FieldName: "", InputType: "VARCHAR", IsMandatory: true}},
	}}}

	if errs := ValidateAnswer(schema, map[string]interface{}{}); len(errs) != 0 {
		t.Fatalf("want question with empty fieldName skipped, got %v", errs)
	}
}

func TestValidateAnswer_UnknownAnswerKeysAreIgnored(t *testing.T) {
	// task_id gets stuffed into every answer by submitAnswerForUser before
	// it ever reaches a validator — make sure that doesn't trip this up.
	schema := FormSchema{Sections: []Section{{
		Questions: []Question{{FieldName: "note", InputType: "VARCHAR", IsMandatory: true}},
	}}}

	answer := map[string]interface{}{"note": "ok", "task_id": "t1", "unexpected": "value"}
	if errs := ValidateAnswer(schema, answer); len(errs) != 0 {
		t.Fatalf("want extra answer keys ignored, got %v", errs)
	}
}

// This one's here so a future "let's clean up the json tags" pass notices
// immediately if it breaks decoding — the shape has to stay byte-for-byte
// compatible with what web-backend actually sends, not what looks tidier.
func TestFormSchema_DecodesRealKotlinShape(t *testing.T) {
	raw := `{
		"sections": [
			{
				"isActive": true,
				"sortOrder": 1,
				"questions": [
					{
						"fieldName": "grade",
						"label": "เกรด",
						"inputType": "OPTION",
						"isMandatory": true,
						"isActive": true,
						"choices": [{"id": 1, "name": "Grade A"}]
					}
				]
			}
		]
	}`

	var schema FormSchema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(schema.Sections) != 1 || len(schema.Sections[0].Questions) != 1 {
		t.Fatalf("unexpected decode shape: %+v", schema)
	}
	q := schema.Sections[0].Questions[0]
	if q.FieldName != "grade" || q.InputType != "OPTION" || !q.IsMandatory {
		t.Errorf("question fields decoded incorrectly: %+v", q)
	}
	if len(q.Choices) != 1 || q.Choices[0].Name != "Grade A" {
		t.Errorf("choices decoded incorrectly: %+v", q.Choices)
	}
}
