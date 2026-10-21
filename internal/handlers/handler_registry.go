package handlers

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"go-server-mobile/internal/validation"

	"gorm.io/gorm"
)

// US2-13 (docs-and-plan#242): which tables a form can write into, and which
// answers depend on which, now come from form.handler_registry and
// form.field_dependency (database V30) instead of the standaloneHandlerTables
// and staleParentFields maps this service used to keep in sync by hand with
// web-backend and the chatbot. Adding a handler is a migration, not a deploy.
//
// Read through a small in-process cache with the same 5-minute TTL as
// formSchemaCache: the rows change only by migration, so re-reading them on
// every submission would be pure overhead, but a TTL (not a forever cache)
// lets a newly migrated handler start working without a restart.
const handlerRegistryTTL = 5 * time.Minute

// identifierPattern guards every schema/table/column name taken from the
// registry: they end up in dynamic SQL (dissectAnswer's INSERT, the
// dependency check below). Only migrations can write those rows, and the
// migration has a CHECK for the same pattern -- this is defence in depth.
var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

type fieldDependency struct {
	ChildField  string
	ParentField string
	// LinkTable is schema-qualified ("agriculture.plot"); LinkColumn is the
	// column on it pointing at the parent.
	LinkTable  string
	LinkColumn string
	LabelTh    string
}

type handlerRegistry struct {
	// handler -> "schema.table", active rows whose table exists only.
	tables map[string]string
	// Every registry parent_field (active or not): a parent row id that must
	// never be reused by autofill.
	staleParents map[string]bool
	// Handlers with a parent_handler (picked first in chat). Neither channel
	// offers autofill on them -- see GetTaskAutofill.
	childHandlers map[string]bool
	dependencies []fieldDependency
	loadedAt     time.Time
}

func (r *handlerRegistry) table(handler string) (string, bool) {
	table, ok := r.tables[handler]
	return table, ok
}

var (
	registryMu    sync.Mutex
	registryCache *handlerRegistry
)

// loadHandlerRegistry is a variable so tests can swap in a fixed registry
// without a database.
var loadHandlerRegistry = loadHandlerRegistryFromDB

// currentHandlerRegistry returns the cached registry, reloading it once the
// TTL has passed. A failed reload keeps serving nothing stale: the error goes
// back to the caller, which refuses the request rather than guessing which
// tables are valid.
func currentHandlerRegistry(db *gorm.DB) (*handlerRegistry, error) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if registryCache != nil && time.Since(registryCache.loadedAt) < handlerRegistryTTL {
		return registryCache, nil
	}
	reg, err := loadHandlerRegistry(db)
	if err != nil {
		return nil, err
	}
	// Stamped here, not trusted from the loader, so the TTL holds whatever
	// the loader is.
	reg.loadedAt = time.Now()
	registryCache = reg
	return reg, nil
}

// setHandlerRegistryForTest installs a fixed registry (and returns a restore
// func). Test-only.
func setHandlerRegistryForTest(reg *handlerRegistry) func() {
	registryMu.Lock()
	previous, previousLoader := registryCache, loadHandlerRegistry
	reg.loadedAt = time.Now()
	registryCache = reg
	loadHandlerRegistry = func(*gorm.DB) (*handlerRegistry, error) { return reg, nil }
	registryMu.Unlock()
	return func() {
		registryMu.Lock()
		registryCache, loadHandlerRegistry = previous, previousLoader
		registryMu.Unlock()
	}
}

func loadHandlerRegistryFromDB(db *gorm.DB) (*handlerRegistry, error) {
	var existing []struct {
		Qualified string `gorm:"column:qualified"`
	}
	if err := db.Raw(
		`SELECT table_schema || '.' || table_name AS qualified FROM information_schema.tables`,
	).Scan(&existing).Error; err != nil {
		return nil, fmt.Errorf("read information_schema.tables: %w", err)
	}
	tableExists := make(map[string]bool, len(existing))
	for _, e := range existing {
		tableExists[e.Qualified] = true
	}

	var rows []struct {
		Handler       string  `gorm:"column:handler"`
		SchemaName    string  `gorm:"column:schema_name"`
		TableName     string  `gorm:"column:table_name"`
		ParentHandler *string `gorm:"column:parent_handler"`
		ParentField   *string `gorm:"column:parent_field"`
		IsActive      bool    `gorm:"column:is_active"`
	}
	if err := db.Raw(
		`SELECT handler, schema_name, table_name, parent_handler, parent_field, is_active FROM form.handler_registry`,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read form.handler_registry: %w", err)
	}

	reg := &handlerRegistry{
		tables:        map[string]string{},
		staleParents:  map[string]bool{},
		childHandlers: map[string]bool{},
		loadedAt:      time.Now(),
	}
	for _, row := range rows {
		if row.ParentField != nil && identifierPattern.MatchString(*row.ParentField) {
			reg.staleParents[*row.ParentField] = true
		}
		if row.ParentHandler != nil {
			reg.childHandlers[row.Handler] = true
		}
		if !row.IsActive {
			continue
		}
		qualified := row.SchemaName + "." + row.TableName
		switch {
		case !identifierPattern.MatchString(row.SchemaName) || !identifierPattern.MatchString(row.TableName):
			slog.Error("handler_registry row has an invalid identifier -- ignored", "handler", row.Handler)
		case !tableExists[qualified]:
			slog.Error("handler_registry row points at a missing table -- ignored", "handler", row.Handler, "table", qualified)
		default:
			reg.tables[row.Handler] = qualified
		}
	}

	var deps []fieldDependency
	if err := db.Raw(
		`SELECT child_field, parent_field, link_table, link_column, label_th FROM form.field_dependency ORDER BY child_field`,
	).Scan(&deps).Error; err != nil {
		return nil, fmt.Errorf("read form.field_dependency: %w", err)
	}
	for _, d := range deps {
		if !validDependency(d) || !tableExists[d.LinkTable] {
			slog.Error("field_dependency row is invalid or points at a missing table -- ignored", "child_field", d.ChildField)
			continue
		}
		reg.dependencies = append(reg.dependencies, d)
	}
	return reg, nil
}

func validDependency(d fieldDependency) bool {
	parts := strings.Split(d.LinkTable, ".")
	return len(parts) == 2 &&
		identifierPattern.MatchString(parts[0]) && identifierPattern.MatchString(parts[1]) &&
		identifierPattern.MatchString(d.ChildField) && identifierPattern.MatchString(d.ParentField) &&
		identifierPattern.MatchString(d.LinkColumn)
}

// quoteIdent double-quotes an identifier that already matched
// identifierPattern (so it can contain no quote of its own).
func quoteIdent(name string) string {
	return `"` + name + `"`
}

// dependencyChecker returns the submit-time check from the design doc (§3.2
// point 4): when an answer carries both a dependent field and its parent,
// the chosen child must actually belong to the chosen parent -- a plot from
// another farm is rejected with a field error instead of being saved against
// the wrong farm. Clients filter the choices already (Kotlin sends parentId),
// so this only catches a stale screen, an old app version, or a hand-made
// request. A pair with either side missing or non-scalar is skipped: the
// required-field and per-type checks are someone else's job.
func dependencyChecker(db *gorm.DB, deps []fieldDependency) func(map[string]interface{}) ([]validation.FieldError, error) {
	return func(answer map[string]interface{}) ([]validation.FieldError, error) {
		var errs []validation.FieldError
		for _, d := range deps {
			child, childOK := scalarAnswer(answer[d.ChildField])
			parent, parentOK := scalarAnswer(answer[d.ParentField])
			if !childOK || !parentOK {
				continue
			}
			parts := strings.SplitN(d.LinkTable, ".", 2)
			query := fmt.Sprintf(
				`SELECT EXISTS (SELECT 1 FROM %s.%s WHERE %s::text = ? AND %s::text = ?)`,
				quoteIdent(parts[0]), quoteIdent(parts[1]), quoteIdent(d.ChildField), quoteIdent(d.LinkColumn),
			)
			var belongs bool
			if err := db.Raw(query, child, parent).Row().Scan(&belongs); err != nil {
				return nil, fmt.Errorf("check %s -> %s: %w", d.ChildField, d.ParentField, err)
			}
			if !belongs {
				errs = append(errs, validation.FieldError{FieldName: d.ChildField, Message: d.LabelTh})
			}
		}
		return errs, nil
	}
}

// scalarAnswer turns a single answer value into the text form compared in
// SQL, or reports that it isn't a single value (absent, empty, a list...).
func scalarAnswer(value interface{}) (string, bool) {
	switch v := value.(type) {
	case string:
		v = strings.TrimSpace(v)
		return v, v != ""
	case float64:
		return fmt.Sprintf("%v", v), true
	default:
		return "", false
	}
}
