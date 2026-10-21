package handlers

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// v30Registry is form.handler_registry as database V30 seeds it, reduced to
// what this service reads: the 10 legacy handlers' tables (exactly
// legacyHandlerTables), the 3 new ones, the 5 child handlers and their 3
// parent fields, and the plot -> farm dependency. Unit tests install it with
// setHandlerRegistryForTest instead of reading a database.
func v30Registry() *handlerRegistry {
	tables := map[string]string{}
	for handler, table := range legacyHandlerTables {
		tables[handler] = table
	}
	tables["farm_initial_cost"] = "agriculture.farm_initial_cost"
	tables["farm_economic_eval"] = "agriculture.farm_economic_eval"
	tables["plot_breed"] = "agriculture.plot_breed"
	return &handlerRegistry{
		tables:       tables,
		staleParents: map[string]bool{"farm_activity_id": true, "harvest_id": true, "batch_id": true},
		childHandlers: map[string]bool{
			"farm_activity_fertilizer": true, "farm_activity_chemical": true,
			"harvest_grade_detail": true, "fermentation_batch": true, "drying_batch": true,
		},
		dependencies: []fieldDependency{
			{ChildField: "plot_id", ParentField: "farm_id", LinkTable: "agriculture.plot", LinkColumn: "farm_id", LabelTh: "แปลงต้องอยู่ในฟาร์มที่เลือก"},
		},
	}
}

func TestRegistry_LegacyHandlersResolveToTheSameTables(t *testing.T) {
	reg := v30Registry()
	for handler, want := range legacyHandlerTables {
		got, ok := reg.table(handler)
		if !ok || got != want {
			t.Errorf("handler %q: want %q, got %q (found=%v)", handler, want, got, ok)
		}
	}
}

func TestRegistry_UnknownHandlerIsNotSupported(t *testing.T) {
	// Same 501 path as before: a handler the registry doesn't have.
	if _, ok := v30Registry().table("not_a_real_handler"); ok {
		t.Fatal("an unknown handler must not resolve to a table")
	}
}

func TestCurrentHandlerRegistry_IsCachedUntilTheTTL(t *testing.T) {
	calls := 0
	registryMu.Lock()
	previous, previousLoader := registryCache, loadHandlerRegistry
	registryCache = nil
	loadHandlerRegistry = func(_ *gorm.DB) (*handlerRegistry, error) {
		calls++
		return v30Registry(), nil
	}
	registryMu.Unlock()
	defer func() {
		registryMu.Lock()
		registryCache, loadHandlerRegistry = previous, previousLoader
		registryMu.Unlock()
	}()

	for i := 0; i < 3; i++ {
		if _, err := currentHandlerRegistry(nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("want 1 load within the TTL, got %d", calls)
	}
}

func TestCurrentHandlerRegistry_LoadFailureIsReturned(t *testing.T) {
	registryMu.Lock()
	previous, previousLoader := registryCache, loadHandlerRegistry
	registryCache = nil
	loadHandlerRegistry = func(_ *gorm.DB) (*handlerRegistry, error) { return nil, errors.New("db down") }
	registryMu.Unlock()
	defer func() {
		registryMu.Lock()
		registryCache, loadHandlerRegistry = previous, previousLoader
		registryMu.Unlock()
	}()

	if _, err := currentHandlerRegistry(nil); err == nil {
		t.Fatal("a failed load must surface as an error, not an empty registry")
	}
}

func TestValidDependency_RejectsUnsafeIdentifiers(t *testing.T) {
	ok := fieldDependency{ChildField: "plot_id", ParentField: "farm_id", LinkTable: "agriculture.plot", LinkColumn: "farm_id"}
	if !validDependency(ok) {
		t.Fatal("the real plot -> farm row must be valid")
	}
	for _, bad := range []fieldDependency{
		{ChildField: "plot_id", ParentField: "farm_id", LinkTable: "plot", LinkColumn: "farm_id"},
		{ChildField: "plot_id", ParentField: "farm_id", LinkTable: `agriculture.plot"; DROP TABLE x;--`, LinkColumn: "farm_id"},
		{ChildField: "Plot", ParentField: "farm_id", LinkTable: "agriculture.plot", LinkColumn: "farm_id"},
	} {
		if validDependency(bad) {
			t.Errorf("must be rejected: %+v", bad)
		}
	}
}

func TestScalarAnswer(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string
		ok   bool
	}{
		{"  abc ", "abc", true},
		{"", "", false},
		{float64(3), "3", true},
		{nil, "", false},
		{[]interface{}{"a", "b"}, "", false},
	}
	for _, c := range cases {
		got, ok := scalarAnswer(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("scalarAnswer(%v) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestInsertViolationError_NotNullBecomesAFieldErrorWithTheLabel(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "23502", ColumnName: "hub_id"})

	fe, ok := insertViolationError(err, map[string]string{"hub_id": "จุดรวบรวม"})

	if !ok || fe.FieldName != "hub_id" || !strings.Contains(fe.Message, "'จุดรวบรวม'") {
		t.Fatalf("got %+v ok=%v", fe, ok)
	}
}

func TestInsertViolationError_ForeignKeyReadsTheColumnFromTheDetail(t *testing.T) {
	err := &pgconn.PgError{
		Code:   "23503",
		Detail: `Key (farm_id)=(11111111-0000-0000-0000-000000000000) is not present in table "farm".`,
	}

	fe, ok := insertViolationError(err, nil)

	if !ok || fe.FieldName != "farm_id" || !strings.Contains(fe.Message, "ไม่มีอยู่ในระบบ") {
		t.Fatalf("got %+v ok=%v", fe, ok)
	}
}

func TestInsertViolationError_TranslatedForeignKeyIsStillAFieldError(t *testing.T) {
	// What the real app connection produces (TranslateError: true): the
	// Postgres detail is gone, so no column -- but still a 400, not a 500.
	fe, ok := insertViolationError(fmt.Errorf("insert: %w", gorm.ErrForeignKeyViolated), nil)

	if !ok || !strings.Contains(fe.Message, "ไม่มีอยู่ในระบบ") {
		t.Fatalf("got %+v ok=%v", fe, ok)
	}
}

func TestInsertViolationError_AnythingElseStaysAServerError(t *testing.T) {
	if _, ok := insertViolationError(&pgconn.PgError{Code: "23505"}, nil); ok {
		t.Fatal("a unique violation is not a farmer's mistake -- must stay a 500")
	}
	if _, ok := insertViolationError(errors.New("connection reset"), nil); ok {
		t.Fatal("a non-Postgres error must stay a 500")
	}
}
