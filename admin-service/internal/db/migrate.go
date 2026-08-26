// Package db applies the schema migrations that own admin_contact_db.
package db

import (
	"embed"
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	// Registers the "pgx5" database driver that the migration URL below resolves to.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
)

// The migrations are embedded so the binary carries its own schema: the runtime image
// needs no SQL files on disk and cannot drift from the code that was built with them.
//
//go:embed migrations/*.sql
var migrations embed.FS

// Migrate brings admin_contact_db up to the latest version. Mirrors the Java services'
// arrangement where Flyway owns the schema outright - nothing here creates or alters a
// table outside a migration file.
func Migrate(databaseURL string) error {
	source, err := iofs.New(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("loading embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, "pgx5://"+trimScheme(databaseURL))
	if err != nil {
		return fmt.Errorf("initialising migrations: %w", err)
	}
	defer func() {
		if sourceErr, dbErr := m.Close(); sourceErr != nil || dbErr != nil {
			slog.Warn("closing migrator", "sourceError", sourceErr, "dbError", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("applying migrations: %w", err)
	}

	slog.Info("database schema is up to date")
	return nil
}

// trimScheme strips the postgres:// prefix so the caller can pass one URL that both
// pgxpool and golang-migrate understand.
func trimScheme(databaseURL string) string {
	for _, prefix := range []string{"postgres://", "postgresql://", "pgx5://", "pgx://"} {
		if len(databaseURL) >= len(prefix) && databaseURL[:len(prefix)] == prefix {
			return databaseURL[len(prefix):]
		}
	}
	return databaseURL
}
