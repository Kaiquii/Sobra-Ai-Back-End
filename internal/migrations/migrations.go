package migrations

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	migrationsTable = "schema_migrations"
	migrationLockID = 764203126
)

var (
	ErrBaselineRequired  = errors.New("banco existente sem baseline; revise o schema e execute o comando baseline antes de ativar migrations versionadas")
	ErrPendingMigrations = errors.New("banco possui migrations pendentes")
)

type Migration struct {
	Version  int64
	Name     string
	Checksum string
	Up       func(*gorm.DB) error
}

type AppliedMigration struct {
	Version   int64     `gorm:"primaryKey"`
	Name      string    `gorm:"size:255;not null"`
	Checksum  string    `gorm:"size:64;not null"`
	AppliedAt time.Time `gorm:"not null;autoCreateTime"`
}

func (AppliedMigration) TableName() string {
	return migrationsTable
}

type StatusEntry struct {
	Version   int64
	Name      string
	Applied   bool
	AppliedAt *time.Time
}

var registry []Migration

func init() {
	registry = []Migration{
		{Version: 1, Name: "baseline_current_schema", Checksum: baselineRecordedChecksum, Up: migrateBaselineSchema},
		{Version: 2, Name: "add_expense_payment_status", Checksum: "7bab1d036f6a54b35e3949df0cd2bb9c1d017396209a6abf886f633b7293e7c6", Up: addExpensePaymentStatus},
		{Version: 3, Name: "add_expense_payment_splits", Checksum: "4a708d6cbbb9445a2d1d4e79a0d51e2d1f50be3032a78a86fc7a27a8c03e8f12", Up: addExpensePaymentSplits},
		{Version: 4, Name: "add_expense_advance_status", Checksum: "d45c5267cbff91235a63a42195c9ffb306c6d24f75b0a821a0f04826b3dfc8ef", Up: addExpenseAdvanceStatus},
		{Version: 5, Name: "add_user_last_active", Checksum: "3ad160234f946be9008713d3264a2aef31c16a1b6605b1dcd45803a3ec94ddb6", Up: addUserLastActive},
		{Version: 6, Name: "add_patrimony", Checksum: "e36eee162ed6f93b0e9ee0e528edb522fde74cd103515e03f8a17b5ffb9c1877", Up: addPatrimony},
		{Version: 7, Name: "add_investment_categories", Checksum: "7516246c77df8f5cbad3ffad6e69d02dca10d663c6af45cb61ee33f68bddeb64", Up: addInvestmentCategories},
		{Version: 8, Name: "add_investment_category_cdi", Checksum: "1621a5189f65a3e22865ddee7176df41984c6d9cc18d1e20c46977cf9edff7c0", Up: addInvestmentCategoryCDI},
	}
}

func Up(db *gorm.DB) error {
	if db == nil {
		return errors.New("conexao com o banco nao inicializada")
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := acquireLock(tx); err != nil {
			return err
		}
		if err := ensureMigrationsTable(tx); err != nil {
			return err
		}

		applied, err := loadApplied(tx)
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			exists, err := applicationSchemaExists(tx)
			if err != nil {
				return err
			}
			if exists {
				return ErrBaselineRequired
			}
		}
		if err := validateAppliedMigrations(applied); err != nil {
			return err
		}

		for _, migration := range pendingMigrations(applied) {
			if err := migration.Up(tx); err != nil {
				return fmt.Errorf("falha ao aplicar migration %06d_%s: %w", migration.Version, migration.Name, err)
			}
			if err := tx.Create(&AppliedMigration{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum}).Error; err != nil {
				return fmt.Errorf("falha ao registrar migration %06d_%s: %w", migration.Version, migration.Name, err)
			}
		}
		return nil
	})
}

// Validate checks that the database is fully compatible with this binary without
// creating tables, acquiring write locks, or applying migrations.
func Validate(db *gorm.DB) error {
	if db == nil {
		return errors.New("conexao com o banco nao inicializada")
	}

	exists, err := migrationsTableExists(db)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("tabela %s ausente; execute ./migrate up antes de iniciar a API", migrationsTable)
	}

	applied, err := loadApplied(db)
	if err != nil {
		return err
	}
	if err := validateAppliedMigrations(applied); err != nil {
		return err
	}

	pending := pendingMigrations(applied)
	if len(pending) == 0 {
		return nil
	}

	names := make([]string, 0, len(pending))
	for _, migration := range pending {
		names = append(names, fmt.Sprintf("%06d_%s", migration.Version, migration.Name))
	}
	return fmt.Errorf("%w: %s; execute ./migrate up antes de iniciar a API", ErrPendingMigrations, strings.Join(names, ", "))
}

func Baseline(db *gorm.DB) error {
	if db == nil {
		return errors.New("conexao com o banco nao inicializada")
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := acquireLock(tx); err != nil {
			return err
		}
		if err := validateBaselineSchema(tx); err != nil {
			return fmt.Errorf("schema atual nao corresponde a baseline: %w", err)
		}
		if err := ensureMigrationsTable(tx); err != nil {
			return err
		}

		applied, err := loadApplied(tx)
		if err != nil {
			return err
		}
		if len(applied) > 0 {
			return errors.New("baseline recusada porque ja existem migrations registradas")
		}

		baseline := sortedRegistry()[0]
		if baseline.Version != 1 {
			return errors.New("primeira migration registrada nao e a baseline")
		}
		if err := tx.Create(&AppliedMigration{Version: baseline.Version, Name: baseline.Name, Checksum: baseline.Checksum}).Error; err != nil {
			return fmt.Errorf("falha ao registrar baseline: %w", err)
		}
		return nil
	})
}

func Status(db *gorm.DB) ([]StatusEntry, error) {
	if db == nil {
		return nil, errors.New("conexao com o banco nao inicializada")
	}

	applied := map[int64]AppliedMigration{}
	exists, err := migrationsTableExists(db)
	if err != nil {
		return nil, err
	}
	if exists {
		var err error
		applied, err = loadApplied(db)
		if err != nil {
			return nil, err
		}
		if err := validateAppliedMigrations(applied); err != nil {
			return nil, err
		}
	}

	entries := make([]StatusEntry, 0, len(registry))
	for _, migration := range sortedRegistry() {
		entry := StatusEntry{Version: migration.Version, Name: migration.Name}
		if record, ok := applied[migration.Version]; ok {
			entry.Applied = true
			entry.AppliedAt = &record.AppliedAt
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func migrationsTableExists(db *gorm.DB) (bool, error) {
	var exists bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", "public."+migrationsTable).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("falha ao verificar tabela de migrations: %w", err)
	}
	return exists, nil
}

func ensureMigrationsTable(db *gorm.DB) error {
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			checksum VARCHAR(64) NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`).Error; err != nil {
		return fmt.Errorf("falha ao preparar tabela de migrations: %w", err)
	}
	return nil
}

func acquireLock(db *gorm.DB) error {
	if err := db.Exec("SELECT pg_advisory_xact_lock(?)", migrationLockID).Error; err != nil {
		return fmt.Errorf("falha ao obter lock de migrations: %w", err)
	}
	return nil
}

func loadApplied(db *gorm.DB) (map[int64]AppliedMigration, error) {
	var records []AppliedMigration
	if err := db.Order("version asc").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("falha ao consultar migrations aplicadas: %w", err)
	}

	applied := make(map[int64]AppliedMigration, len(records))
	for _, record := range records {
		applied[record.Version] = record
	}
	return applied, nil
}

func sortedRegistry() []Migration {
	migrations := append([]Migration(nil), registry...)
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations
}

func validateAppliedMigrations(applied map[int64]AppliedMigration) error {
	known := make(map[int64]Migration, len(registry))
	for _, migration := range registry {
		known[migration.Version] = migration
	}
	for version, record := range applied {
		migration, ok := known[version]
		if !ok {
			return fmt.Errorf("banco possui migration desconhecida %06d; o binario pode estar desatualizado", version)
		}
		if record.Name != migration.Name {
			return fmt.Errorf("migration %d registrada como %q, mas o codigo atual espera %q", migration.Version, record.Name, migration.Name)
		}
		if record.Checksum != migration.Checksum {
			return fmt.Errorf("checksum da migration %06d_%s foi alterado", migration.Version, migration.Name)
		}
	}
	return nil
}

func pendingMigrations(applied map[int64]AppliedMigration) []Migration {
	pending := make([]Migration, 0)
	for _, migration := range sortedRegistry() {
		if _, ok := applied[migration.Version]; !ok {
			pending = append(pending, migration)
		}
	}
	return pending
}
