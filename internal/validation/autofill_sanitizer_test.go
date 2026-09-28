package validation

import "testing"

func TestSanitizeAutofillAnswer_DropsTaskIdAndStaleParentFieldsUnconditionally(t *testing.T) {
	answer := map[string]interface{}{
		"task_id":          "t-1",
		"farm_activity_id": "fa-1",
		"harvest_id":       "h-1",
		"batch_id":         "b-1",
		"note":             "hello",
	}
	questions := []Question{{FieldName: "note", InputType: "VARCHAR"}}

	got := SanitizeAutofillAnswer(answer, questions)

	want := map[string]interface{}{"note": "hello"}
	assertAnswerEqual(t, got, want)
}

func TestSanitizeAutofillAnswer_FreeTextFieldPassesThroughUnchanged(t *testing.T) {
	answer := map[string]interface{}{"note": "ปุ๋ยอินทรีย์"}
	questions := []Question{{FieldName: "note", InputType: "VARCHAR"}}

	got := SanitizeAutofillAnswer(answer, questions)

	assertAnswerEqual(t, got, answer)
}

func TestSanitizeAutofillAnswer_OptionValueStillOnFormIsKept(t *testing.T) {
	answer := map[string]interface{}{"fertilizer_id": "f-1"}
	questions := []Question{{
		FieldName: "fertilizer_id", InputType: "OPTION",
		Choices: []Choice{{ID: "f-1", Name: "label-f-1"}},
	}}

	got := SanitizeAutofillAnswer(answer, questions)

	assertAnswerEqual(t, got, answer)
}

func TestSanitizeAutofillAnswer_StaleOptionValueIsDropped(t *testing.T) {
	// e.g. that fertilizer was deleted/renamed since the last submission --
	// its old id no longer resolves to any real choice on this form.
	answer := map[string]interface{}{"fertilizer_id": "stale-id"}
	questions := []Question{{
		FieldName: "fertilizer_id", InputType: "OPTION",
		Choices: []Choice{{ID: "current-id", Name: "current"}},
	}}

	got := SanitizeAutofillAnswer(answer, questions)

	assertAnswerEqual(t, got, map[string]interface{}{})
}

func TestSanitizeAutofillAnswer_BooleanValueMatchingSynthesizedChoiceIsKept(t *testing.T) {
	answer := map[string]interface{}{"is_quality_damage": "true"}
	questions := []Question{{FieldName: "is_quality_damage", InputType: "BOOLEAN"}}

	got := SanitizeAutofillAnswer(answer, questions)

	assertAnswerEqual(t, got, answer)
}

func TestSanitizeAutofillAnswer_BooleanValueNotMatchingIsDropped(t *testing.T) {
	answer := map[string]interface{}{"is_quality_damage": "maybe"}
	questions := []Question{{FieldName: "is_quality_damage", InputType: "BOOLEAN"}}

	got := SanitizeAutofillAnswer(answer, questions)

	assertAnswerEqual(t, got, map[string]interface{}{})
}

func TestSanitizeAutofillAnswer_FieldNotOnCurrentFormPassesThrough(t *testing.T) {
	// Mirrors chatbot's own reuse.sanitize_for_autofill: an unrecognized
	// free-text-shaped field name isn't this function's job to drop --
	// that's the caller's build_answer_rows step, which looks the field up
	// against the current form's real questions.
	answer := map[string]interface{}{"retired_field": "x"}
	questions := []Question{{FieldName: "note", InputType: "VARCHAR"}}

	got := SanitizeAutofillAnswer(answer, questions)

	assertAnswerEqual(t, got, answer)
}

func TestSanitizeAutofillAnswer_MixedAnswerOnlyDropsWhatItShould(t *testing.T) {
	answer := map[string]interface{}{
		"task_id":       "t-1",
		"batch_id":      "b-1",
		"note":          "ok",
		"fertilizer_id": "stale-id",
	}
	questions := []Question{
		{FieldName: "note", InputType: "VARCHAR"},
		{FieldName: "fertilizer_id", InputType: "OPTION", Choices: []Choice{{ID: "current-id", Name: "current"}}},
	}

	got := SanitizeAutofillAnswer(answer, questions)

	assertAnswerEqual(t, got, map[string]interface{}{"note": "ok"})
}

func assertAnswerEqual(t *testing.T, got, want map[string]interface{}) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d fields %v, want %d fields %v", len(got), got, len(want), want)
	}
	for k, wantV := range want {
		gotV, ok := got[k]
		if !ok {
			t.Fatalf("missing field %q in result %v", k, got)
		}
		if gotV != wantV {
			t.Fatalf("field %q: got %v, want %v", k, gotV, wantV)
		}
	}
}
