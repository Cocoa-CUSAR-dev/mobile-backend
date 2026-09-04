// #105 (US2-5): one shared implementation of "what's safe to prefill a
// farmer with from their last submission" -- called by both the chatbot
// (src/conversation/reuse.py's fetch_sanitized_autofill) and, eventually,
// any remaining static form screen, so the two channels can't drift apart
// on the rules. Lives in this package (not handlers) because it reuses
// Question/Choice/choiceExists from answer_validator.go and needs no DB
// access at all -- it's a pure function of its inputs.
package validation

import "strings"

// nonAnswerFields are keys a raw last-submission answer can carry that are
// never real form fields (e.g. Go's own echoed-back task_id) -- never safe
// to reuse regardless of what the current form looks like.
var nonAnswerFields = map[string]bool{
	"task_id": true,
}

// staleParentFields are the previously-blocked handlers' parent-picker
// fields (see database's V13 migration / chatbot's src/line/parent_picker.py).
// Reusing one blind would silently attach a new submission to an OLD parent
// row -- a different farm activity/harvest/batch than this submission is
// actually about -- a data-integrity bug invisible from either UI, since
// nothing displays the raw id. Always dropped, regardless of form shape.
var staleParentFields = map[string]bool{
	"farm_activity_id": true,
	"harvest_id":       true,
	"batch_id":         true,
}

// booleanChoices -- Kotlin never sends `choices` for a BOOLEAN question
// (its own filter is inputType == OPTION only, see answer_validator.go's
// ValidateAnswer), so these are synthesized here. Same two ids the
// chatbot's own guided-flow engine uses for BOOLEAN questions
// (service.py's _BOOLEAN_CHOICES) -- keep them in sync if that ever changes.
var booleanChoices = []Choice{{ID: "true", Name: "ใช่"}, {ID: "false", Name: "ไม่"}}

// SanitizeAutofillAnswer strips a raw last-submission answer down to what's
// actually safe to offer a farmer as a prefill on the CURRENT form. Three
// rules:
//
//  1. task_id and the 3 stale parent-id fields are dropped outright, always.
//  2. An OPTION/BOOLEAN field whose stored value doesn't match any of the
//     CURRENT form's real choices for that field is dropped -- a
//     farm/fertilizer/etc. that's since been deleted or renamed shouldn't
//     silently offer a value that no longer resolves to anything real.
//  3. Every other field (free text, or a still-resolving OPTION value)
//     passes through unchanged.
func SanitizeAutofillAnswer(
	answer map[string]interface{}, questions []Question,
) map[string]interface{} {
	choicesByField := map[string][]Choice{}
	for _, q := range questions {
		switch strings.ToUpper(q.InputType) {
		case "OPTION":
			choicesByField[q.FieldName] = q.Choices
		case "BOOLEAN":
			choicesByField[q.FieldName] = booleanChoices
		}
	}

	sanitized := map[string]interface{}{}
	for field, value := range answer {
		if nonAnswerFields[field] || staleParentFields[field] {
			continue
		}
		if choices, isConstrained := choicesByField[field]; isConstrained && !choiceExists(choices, value) {
			continue
		}
		sanitized[field] = value
	}
	return sanitized
}
