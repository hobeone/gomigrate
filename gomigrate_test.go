package gomigrate

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

var (
	db         *sql.DB
	adapter    Migratable
	dbType     string
	nullLogger = log.New(ioutil.Discard, "", log.LstdFlags)
)

func GetMigrator(test string) *Migrator {
	path := fmt.Sprintf("test_migrations/%s_%s", test, dbType)
	m, err := NewMigratorWithLogger(db, adapter, path, nullLogger)
	if err != nil {
		panic(err)
	}
	return m
}

func TestNewMigratorFromMemory(t *testing.T) {
	migrations := []*Migration{
		{
			ID:   100,
			Name: "FirstMigration",
			Up: `CREATE TABLE MemoryTest (
				id INTEGER PRIMARY KEY
			)`,
			Down: `drop table "MemoryTest"`,
		},
		{
			ID:   110,
			Name: "SecondMigration",
			Up: `CREATE TABLE MemoryTest2 (
				id INTEGER PRIMARY KEY
			)`,
			Down: `drop table "MemoryTest2"`,
		},
	}
	m, err := NewMigratorWithMigrations(db, adapter, migrations)
	if err != nil {
		t.Fatalf("Error makiing new migrator: %v", err)
	}
	m.Logger = nullLogger
	m.Migrate()
}

func TestGetMigrationsFromPath(t *testing.T) {
	path := fmt.Sprintf("test_migrations/%s_%s/", "test1", "pg")
	m, err := MigrationsFromPath(path, nullLogger)
	if err != nil {
		t.Fatalf("Error getting migrations: %v", err)
	}
	if len(m) < 4 {
		t.Fatalf("Expected 4 migrations, got %d", len(m))
	}
}

func TestNewMigrator(t *testing.T) {
	m := GetMigrator("test1")
	switch {
	case dbType == "pg" && len(m.migrations) != 4:
		t.Errorf("Invalid number of migrations detected")

	case dbType == "mysql" && len(m.migrations) != 1:
		t.Errorf("Invalid number of migrations detected")

	case dbType == "sqlite3" && len(m.migrations) != 1:
		t.Errorf("Invalid number of migrations detected")
	}

	migration := m.migrations[1]

	if migration.Name != "test" {
		t.Errorf("Invalid migration name detected: %s", migration.Name)
	}
	if migration.ID != 1 {
		t.Errorf("Invalid migration num detected: %d", migration.ID)
	}
	if migration.Status != Inactive {
		t.Errorf("Invalid migration num detected: %d", migration.Status)
	}

	cleanup()
}

func TestApplyMigrations(t *testing.T) {
	m := &Migrator{
		Logger: nullLogger,
	}
	err := m.ApplyMigration(&Migration{}, migrationType("foo"))
	if err == nil {
		t.Fatalf("Expected error on invalid migration type")
	}
}

func TestCreatingMigratorWhenTableExists(t *testing.T) {
	// Create the table and populate it with a row.
	_, err := db.Exec(adapter.CreateMigrationTableSql())
	if err != nil {
		t.Error(err)
	}
	_, err = db.Exec(adapter.MigrationLogInsertSql(), 123)
	if err != nil {
		t.Error(err)
	}

	GetMigrator("test1")

	// Check that our row is still present.
	row := db.QueryRow("select migration_id from gomigrate")
	var id uint64
	err = row.Scan(&id)
	if err != nil {
		t.Error(err)
	}
	if id != 123 {
		t.Error("Invalid id found in database")
	}
	cleanup()
}

func TestMigrationAndRollback(t *testing.T) {
	m := GetMigrator("test1")

	if err := m.Migrate(); err != nil {
		t.Error(err)
	}

	// Ensure that the migration ran.
	row := db.QueryRow(
		adapter.SelectMigrationTableSql(),
		"test",
	)
	var tableName string
	if err := row.Scan(&tableName); err != nil {
		t.Error(err)
	}
	if tableName != "test" {
		t.Errorf("Migration table not created")
	}
	// Ensure that the migrate status is correct.
	row = db.QueryRow(
		adapter.GetMigrationSql(),
		1,
	)
	var status int
	if err := row.Scan(&status); err != nil {
		t.Error(err)
	}
	if status != Active || m.migrations[1].Status != Active {
		t.Error("Invalid status for migration")
	}
	if err := m.RollbackN(len(m.migrations)); err != nil {
		t.Error(err)
	}

	// Ensure that the down migration ran.
	row = db.QueryRow(
		adapter.SelectMigrationTableSql(),
		"test",
	)
	err := row.Scan(&tableName)
	if err != nil && err != sql.ErrNoRows {
		t.Errorf("Migration table should be deleted: %v", err)
	}

	// Ensure that the migration log is missing.
	row = db.QueryRow(
		adapter.GetMigrationSql(),
		1,
	)
	if err := row.Scan(&status); err != nil && err != sql.ErrNoRows {
		t.Error(err)
	}
	if m.migrations[1].Status != Inactive {
		t.Errorf("Invalid status for migration, expected: %d, got: %v", Inactive, m.migrations[1].Status)
	}

	cleanup()
}

func cleanup() {
	_, err := db.Exec("drop table gomigrate")
	if err != nil {
		panic(err)
	}
}

func init() {
	var err error

	switch os.Getenv("DB") {
	case "mysql":
		dbType = "mysql"
		log.Print("Using mysql")
		adapter = Mariadb{}
		db, err = sql.Open("mysql", "gomigrate:password@/gomigrate")
	case "sqlite3":
		dbType = "sqlite3"
		log.Print("Using in memory sqlite3")
		adapter = Sqlite3{}
		db, err = sql.Open("sqlite3", "file::memory:?cache=shared")
	default:
		dbType = "pg"
		log.Print("Using postgres")
		adapter = Postgres{}
		db, err = sql.Open("postgres", "host=localhost dbname=gomigrate sslmode=disable")
	}

	if err != nil {
		panic(err)
	}
}
