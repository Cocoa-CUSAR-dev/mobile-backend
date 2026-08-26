// Package validation checks whether a submitted answer matches its form's
// schema (the "does it pass" half of #54). Wiring this in front of the
// actual INSERT is a separate change (#3).
//
// Struct tags are camelCase (not the chatbot's snake_case) since this
// decodes web-backend's raw JSON. Shape copied from mobile-app's
// dynamic_register_page.dart, the only current consumer.
package validation

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Choice is one dropdown option on an OPTION question — {id, name}, nothing
// fancier.
type Choice struct {
	ID   interface{} `json:"id"`
	Name string      `json:"name"`
}

// Question is one entry in sections[].questions[]. Kotlin sends more fields
// than this (sortOrder, etc.) but nothing here needs them, so they're left
// off rather than carried around unused.
type Question struct {
	FieldName   string   `json:"fieldName"`
	Label       string   `json:"label"`
	InputType   string   `json:"inputType"`
	IsMandatory bool     `json:"isMandatory"`
	IsActive    *bool    `json:"isActive"`
	Choices     []Choice `json:"choices"`
}

// Active has to match mobile-app's own `isActive != false` check: a
// question with the flag simply absent still counts as shown, only an
// explicit false (researcher toggled it off) hides it. A pointer here isn't
// overkill — a plain bool would zero-value to false and start treating
// every un-set question as hidden, which is the opposite of what we want.
func (q Question) Active() bool {
	return q.IsActive == nil || *q.IsActive
}

// Section is one entry in sections[].
type Section struct {
	IsActive  *bool      `json:"isActive"`
	Questions []Question `json:"questions"`
}

// Active — same reasoning as Question.Active.
func (s Section) Active() bool {
	return s.IsActive == nil || *s.IsActive
}

// FormSchema is what's left of web-backend's GET /forms/{formId} response
// after unwrapping its {value, error} envelope.
type FormSchema struct {
	Sections []Section `json:"sections"`
}

// FieldError describes one answer field that failed validation.
type FieldError struct {
	FieldName string
	Message   string
}

func (e FieldError) Error() string {
	return fmt.Sprintf("%s: %s", e.FieldName, e.Message)
}

// ValidateAnswer walks every active question in schema and checks answer
// against it, returning one FieldError per failing field. Nil back means
// answer is clean.
//
// It doesn't complain about keys in answer that don't belong to any
// question — that's a different problem (a farmer-controlled key trying to
// hit a column it shouldn't), and it's already handled at the DB-column
// layer by filterKnownColumns in form_handler.go. This function only cares
// whether the fields the form actually asked for look right.
func ValidateAnswer(schema FormSchema, answer map[string]interface{}) []FieldError {
	var errs []FieldError
	for _, section := range schema.Sections {
		if !section.Active() {
			continue
		}
		for _, q := range section.Questions {
			if !q.Active() || q.FieldName == "" {
				continue
			}
			if err := validateField(q, answer[q.FieldName]); err != nil {
				errs = append(errs, FieldError{FieldName: q.FieldName, Message: err.Error()})
			}
		}
	}
	return errs
}

func validateField(q Question, value interface{}) error {
	if isEmptyValue(value) {
		if q.IsMandatory {
			return fmt.Errorf("จำเป็นต้องกรอก")
		}
		return nil
	}

	switch strings.ToUpper(q.InputType) {
	case "INT":
		if _, err := toInt(value); err != nil {
			return fmt.Errorf("ต้องเป็นจำนวนเต็ม")
		}
	case "FLOAT":
		if _, err := toFloat(value); err != nil {
			return fmt.Errorf("ต้องเป็นตัวเลข")
		}
	case "DATE":
		if _, err := toDate(value, dateLayouts); err != nil {
			return fmt.Errorf("รูปแบบวันที่ไม่ถูกต้อง")
		}
	case "DATETIME":
		if _, err := toDate(value, dateTimeLayouts); err != nil {
			return fmt.Errorf("รูปแบบวันที่เวลาไม่ถูกต้อง")
		}
	case "BOOLEAN":
		if _, err := toBool(value); err != nil {
			return fmt.Errorf("ต้องเป็นค่า true/false")
		}
	case "OPTION":
		if !choiceExists(q.Choices, value) {
			return fmt.Errorf("ตัวเลือกไม่ถูกต้อง")
		}
	case "GEODATA":
		if !isValidGeodata(value) {
			return fmt.Errorf("ข้อมูลพิกัดไม่ถูกต้อง")
		}
	case "VARCHAR", "":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("ต้องเป็นข้อความ")
		}
	}
	return nil
}

// isEmptyValue is "nothing was answered": nil, a blank/whitespace-only
// string, or an empty slice. Kept in sync with mobile-app's own mandatory
// check in dynamic_register_page.dart's _isCurrentStepValid — if that ever
// changes, this should too.
func isEmptyValue(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []interface{}:
		return len(v) == 0
	default:
		return false
	}
}

// toInt has to accept both a real JSON number (float64, however
// encoding/json decodes it into map[string]interface{}) and a numeric
// string, because mobile-app's number fields are backed by plain text
// controllers and go out over the wire as strings — "42", not 42.
func toInt(value interface{}) (int64, error) {
	switch v := value.(type) {
	case float64:
		if v != float64(int64(v)) {
			return 0, fmt.Errorf("not a whole number")
		}
		return int64(v), nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}

func toFloat(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}

func toBool(value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(strings.TrimSpace(v))
	default:
		return false, fmt.Errorf("unsupported type %T", value)
	}
}

var dateLayouts = []string{"2006-01-02"}

var dateTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func toDate(value interface{}, layouts []string) (time.Time, error) {
	s, ok := value.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("unsupported type %T", value)
	}
	s = strings.TrimSpace(s)
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("no matching layout for %q", s)
}

// choiceExists compares IDs as strings rather than doing a typed comparison,
// because a choice ID decodes as float64 off the schema JSON but can show up
// as a plain string in the answer, depending on which client sent it. Trying
// to compare those directly would just fail every time.
func choiceExists(choices []Choice, value interface{}) bool {
	target := fmt.Sprintf("%v", value)
	for _, c := range choices {
		if fmt.Sprintf("%v", c.ID) == target {
			return true
		}
	}
	return false
}

// isValidGeodata just checks the shape mobile-app already sends for a plot
// boundary: a non-empty list of points, each with a numeric lat and lng.
// Doesn't try to validate the polygon itself (self-intersecting, closed,
// whatever) — that's a different problem than "is this a valid answer".
func isValidGeodata(value interface{}) bool {
	points, ok := value.([]interface{})
	if !ok || len(points) == 0 {
		return false
	}
	for _, p := range points {
		point, ok := p.(map[string]interface{})
		if !ok {
			return false
		}
		if _, err := toFloat(point["lat"]); err != nil {
			return false
		}
		if _, err := toFloat(point["lng"]); err != nil {
			return false
		}
	}
	return true
}
