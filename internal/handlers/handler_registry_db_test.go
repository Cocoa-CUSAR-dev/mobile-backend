package handlers

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// US2-13 (docs-and-plan#242/#244) against a real Postgres: the registry is
// read from a form.handler_registry seeded with database V30's own rows, so
// these prove the switch from the hard-coded maps changed nothing for the 10
// existing handlers, and that a new handler needs no code at all.
//
// Gated on RUN_DB_TESTS like every *_db_test.go here (openIntegrationTestDB).

const handlerRegistryTestSchemaDDL = `
CREATE SCHEMA IF NOT EXISTS form;
CREATE SCHEMA IF NOT EXISTS agriculture;
CREATE SCHEMA IF NOT EXISTS collection;
CREATE SCHEMA IF NOT EXISTS processing;

CREATE TABLE IF NOT EXISTS form.handler_registry (
	handler character varying PRIMARY KEY,
	schema_name character varying NOT NULL,
	table_name character varying NOT NULL,
	label_th character varying NOT NULL,
	description_th character varying NOT NULL,
	category character varying NOT NULL,
	frequency character varying NOT NULL,
	parent_handler character varying REFERENCES form.handler_registry (handler),
	parent_field character varying,
	extra_required_fields character varying[] NOT NULL DEFAULT '{}',
	sort_order integer NOT NULL DEFAULT 0,
	is_active boolean NOT NULL DEFAULT true
);
CREATE TABLE IF NOT EXISTS form.field_dependency (
	child_field character varying NOT NULL,
	parent_field character varying NOT NULL,
	link_table character varying NOT NULL,
	link_column character varying NOT NULL,
	label_th character varying NOT NULL,
	PRIMARY KEY (child_field, parent_field)
);

-- Destination tables: only existence matters to the registry loader, except
-- farm_activity (real NOT NULL columns, for the INSERT-error test) and
-- agriculture.plot (the dependency's link table).
CREATE TABLE IF NOT EXISTS agriculture.plot (
	plot_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	farm_id uuid NOT NULL
);
CREATE TABLE IF NOT EXISTS agriculture.farm_activity (
	farm_activity_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	farm_id uuid NOT NULL,
	plot_id uuid REFERENCES agriculture.plot (plot_id),
	farm_activity_type_id uuid NOT NULL,
	description character varying
);
CREATE TABLE IF NOT EXISTS agriculture.farm_activity_fertilizer (farm_activity_id uuid);
CREATE TABLE IF NOT EXISTS agriculture.farm_activity_chemical (farm_activity_id uuid);
CREATE TABLE IF NOT EXISTS agriculture.farm_pest_disease_record (id uuid);
CREATE TABLE IF NOT EXISTS agriculture.farm_initial_cost (id uuid);
CREATE TABLE IF NOT EXISTS agriculture.farm_economic_eval (id uuid);
CREATE TABLE IF NOT EXISTS agriculture.plot_breed (id uuid);
CREATE TABLE IF NOT EXISTS collection.harvest (id uuid);
CREATE TABLE IF NOT EXISTS collection.harvest_grade_detail (harvest_id uuid);
CREATE TABLE IF NOT EXISTS processing.batch (id uuid);
CREATE TABLE IF NOT EXISTS processing.processing_record (id uuid);
CREATE TABLE IF NOT EXISTS processing.fermentation_batch (batch_id uuid);
CREATE TABLE IF NOT EXISTS processing.drying_batch (batch_id uuid);
`

// The registry rows exactly as database V30 inserts them (columns this
// service doesn't read are filled with placeholders).
const v30RegistrySeed = `
INSERT INTO form.handler_registry (handler, schema_name, table_name, label_th, description_th, category, frequency, parent_handler, parent_field, extra_required_fields, sort_order) VALUES
	('farm_activity', 'agriculture', 'farm_activity', 'x', 'x', 'garden', 'daily', NULL, NULL, '{}', 10),
	('farm_pest_disease_record', 'agriculture', 'farm_pest_disease_record', 'x', 'x', 'garden', 'daily', NULL, NULL, '{}', 40),
	('harvest', 'collection', 'harvest', 'x', 'x', 'harvest', 'per_season', NULL, NULL, '{}', 50),
	('batch', 'processing', 'batch', 'x', 'x', 'processing', 'per_season', NULL, NULL, '{}', 70),
	('processing_record', 'processing', 'processing_record', 'x', 'x', 'processing', 'daily', NULL, NULL, '{}', 80),
	('farm_activity_fertilizer', 'agriculture', 'farm_activity_fertilizer', 'x', 'x', 'garden', 'daily', 'farm_activity', 'farm_activity_id', '{}', 20),
	('farm_activity_chemical', 'agriculture', 'farm_activity_chemical', 'x', 'x', 'garden', 'daily', 'farm_activity', 'farm_activity_id', '{}', 30),
	('harvest_grade_detail', 'collection', 'harvest_grade_detail', 'x', 'x', 'harvest', 'per_season', 'harvest', 'harvest_id', '{}', 60),
	('fermentation_batch', 'processing', 'fermentation_batch', 'x', 'x', 'processing', 'per_season', 'batch', 'batch_id', '{}', 90),
	('drying_batch', 'processing', 'drying_batch', 'x', 'x', 'processing', 'per_season', 'batch', 'batch_id', '{}', 100),
	('farm_initial_cost', 'agriculture', 'farm_initial_cost', 'x', 'x', 'cost', 'once', NULL, NULL, '{}', 110),
	('farm_economic_eval', 'agriculture', 'farm_economic_eval', 'x', 'x', 'cost', 'yearly', NULL, NULL, '{farm_id}', 120),
	('plot_breed', 'agriculture', 'plot_breed', 'x', 'x', 'plot_info', 'once', NULL, NULL, '{}', 130);
INSERT INTO form.field_dependency (child_field, parent_field, link_table, link_column, label_th) VALUES
	('plot_id', 'farm_id', 'agriculture.plot', 'farm_id', 'แปลงต้องอยู่ในฟาร์มที่เลือก');
`

func openRegistryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openIntegrationTestDB(t) // gates on RUN_DB_TESTS
	if err := db.Exec(handlerRegistryTestSchemaDDL).Error; err != nil {
		t.Fatalf("apply registry test schema: %v", err)
	}
	if err := db.Exec(`TRUNCATE form.field_dependency, form.handler_registry, agriculture.plot, agriculture.farm_activity CASCADE`).Error; err != nil {
		t.Fatalf("truncate registry test tables: %v", err)
	}
	if err := db.Exec(v30RegistrySeed).Error; err != nil {
		t.Fatalf("seed V30 registry rows: %v", err)
	}
	return db
}

func TestRegistryDB_V30MatchesEveryHardCodedListItReplaced(t *testing.T) {
	db := openRegistryTestDB(t)

	reg, err := loadHandlerRegistryFromDB(db)
	if err != nil {
		t.Fatal(err)
	}

	// standaloneHandlerTables (this file's legacyHandlerTables): same tables.
	for handler, want := range legacyHandlerTables {
		if got, ok := reg.table(handler); !ok || got != want {
			t.Errorf("handler %q: want %q, got %q (found=%v)", handler, want, got, ok)
		}
	}
	if len(reg.tables) != len(legacyHandlerTables)+3 {
		t.Errorf("want the 10 legacy handlers + 3 new ones, got %d: %v", len(reg.tables), reg.tables)
	}

	// validation.staleParentFields: same 3 parent ids.
	wantStale := map[string]bool{"farm_activity_id": true, "harvest_id": true, "batch_id": true}
	if len(reg.staleParents) != len(wantStale) {
		t.Errorf("want stale parents %v, got %v", wantStale, reg.staleParents)
	}
	for field := range wantStale {
		if !reg.staleParents[field] {
			t.Errorf("stale parent %q missing", field)
		}
	}

	// parentPickerHandlers (and the chatbot's _PARENT_KIND_BY_HANDLER keys):
	// same 5 child handlers.
	wantChildren := []string{
		"farm_activity_fertilizer", "farm_activity_chemical",
		"harvest_grade_detail", "fermentation_batch", "drying_batch",
	}
	if len(reg.childHandlers) != len(wantChildren) {
		t.Errorf("want %d child handlers, got %v", len(wantChildren), reg.childHandlers)
	}
	for _, handler := range wantChildren {
		if !reg.childHandlers[handler] {
			t.Errorf("child handler %q missing", handler)
		}
	}
}

func TestRegistryDB_InactiveOrMissingTableIsNotSupported(t *testing.T) {
	db := openRegistryTestDB(t)
	if err := db.Exec(`UPDATE form.handler_registry SET is_active = false WHERE handler = 'batch'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO form.handler_registry (handler, schema_name, table_name, label_th, description_th, category, frequency)
		VALUES ('ghost', 'agriculture', 'no_such_table', 'x', 'x', 'cost', 'once')`).Error; err != nil {
		t.Fatal(err)
	}

	reg, err := loadHandlerRegistryFromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.table("batch"); ok {
		t.Error("an inactive handler must answer 501, as an unknown one always has")
	}
	if _, ok := reg.table("ghost"); ok {
		t.Error("a row pointing at a missing table must be ignored, not dissected into")
	}
}

func TestRegistryDB_PlotFromAnotherFarmIsAFieldError(t *testing.T) {
	db := openRegistryTestDB(t)
	farmA, farmB, plotOfA := uuid.New(), uuid.New(), uuid.New()
	if err := db.Exec(`INSERT INTO agriculture.plot (plot_id, farm_id) VALUES (?, ?)`, plotOfA, farmA).Error; err != nil {
		t.Fatal(err)
	}
	reg, err := loadHandlerRegistryFromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	check := dependencyChecker(db, reg.dependencies)

	errs, err := check(map[string]interface{}{"farm_id": farmA.String(), "plot_id": plotOfA.String()})
	if err != nil || len(errs) != 0 {
		t.Fatalf("a plot of the chosen farm must pass, got %v / %v", errs, err)
	}

	errs, err = check(map[string]interface{}{"farm_id": farmB.String(), "plot_id": plotOfA.String()})
	if err != nil || len(errs) != 1 || errs[0].FieldName != "plot_id" {
		t.Fatalf("a plot of another farm must be one plot_id error, got %v / %v", errs, err)
	}

	errs, err = check(map[string]interface{}{"plot_id": plotOfA.String()})
	if err != nil || len(errs) != 0 {
		t.Fatalf("no farm answered -> nothing to check against, got %v / %v", errs, err)
	}
}

func TestRegistryDB_NotNullAtInsertBecomesAFarmerReadableFieldError(t *testing.T) {
	db := openRegistryTestDB(t)
	reg, err := loadHandlerRegistryFromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	table, _ := reg.table("farm_activity")

	// A form that never asked farm_activity_type_id (the CB-2/3/4 shape).
	insertErr := db.Transaction(func(tx *gorm.DB) error {
		return dissectAnswer(tx, "farm_activity", table, map[string]interface{}{"farm_id": uuid.New().String()})
	})
	if insertErr == nil {
		t.Fatal("want the NOT NULL violation from the real INSERT")
	}

	fe, ok := insertViolationError(insertErr, map[string]string{})
	if !ok || fe.FieldName != "farm_activity_type_id" {
		t.Fatalf("want a farm_activity_type_id field error, got %+v ok=%v (raw: %v)", fe, ok, insertErr)
	}
}

func TestRegistryDB_ForeignKeyAtInsertIsAFieldErrorThroughTheAppsTranslatedConnection(t *testing.T) {
	// openIntegrationTestDB opens gorm with TranslateError, like the app
	// does, so this is the FK error shape production really sees.
	db := openRegistryTestDB(t)
	reg, err := loadHandlerRegistryFromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	table, _ := reg.table("farm_activity")

	insertErr := db.Transaction(func(tx *gorm.DB) error {
		return dissectAnswer(tx, "farm_activity", table, map[string]interface{}{
			"farm_id":               uuid.New().String(),
			"farm_activity_type_id": uuid.New().String(),
			"plot_id":               uuid.New().String(), // a plot that doesn't exist
		})
	})
	if insertErr == nil {
		t.Fatal("want the FK violation from the real INSERT")
	}
	if _, ok := insertViolationError(insertErr, nil); !ok {
		t.Fatalf("an FK violation must be a 400 field error, not a 500 (raw: %v)", insertErr)
	}
}

func TestRegistryDB_ANewHandlerWorksWithNoCodeChange(t *testing.T) {
	// Design doc §6: a registry row pointing at a table nothing in this
	// service knows about is enough for a submission to dissect into it.
	db := openRegistryTestDB(t)
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agriculture.zz_registry_test_handler (
			row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			note character varying
		);
		TRUNCATE agriculture.zz_registry_test_handler;
		INSERT INTO form.handler_registry (handler, schema_name, table_name, label_th, description_th, category, frequency)
		VALUES ('zz_registry_test_handler', 'agriculture', 'zz_registry_test_handler', 'x', 'x', 'cost', 'once');
	`).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP TABLE IF EXISTS agriculture.zz_registry_test_handler`)
	columnsCache.Delete("agriculture.zz_registry_test_handler")

	reg, err := loadHandlerRegistryFromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	table, ok := reg.table("zz_registry_test_handler")
	if !ok {
		t.Fatal("a migrated registry row must be supported with no code change")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return dissectAnswer(tx, "zz_registry_test_handler", table, map[string]interface{}{"note": "it works", "not_a_column": "dropped"})
	}); err != nil {
		t.Fatal(err)
	}

	var notes []string
	if err := db.Raw(`SELECT note FROM agriculture.zz_registry_test_handler`).Scan(&notes).Error; err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0] != "it works" {
		t.Fatalf("want one dissected row, got %v", notes)
	}
}
