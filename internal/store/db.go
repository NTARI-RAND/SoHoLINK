package store

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps a pgx connection pool. Callers must call db.Pool.Close() when done.
type DB struct {
	Pool *pgxpool.Pool
}

// Connect opens a pgxpool using connString, pings the database to confirm
// reachability, and returns a DB. Pass os.Getenv("DATABASE_URL") as connString.
func Connect(ctx context.Context, connString string) (*DB, error) {
	if connString == "" {
		return nil, fmt.Errorf("connect: connString is empty (is DATABASE_URL set?)")
	}
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("open pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// RunMigrations applies all pending up migrations from internal/store/migrations/.
// It is safe to call on an already up-to-date database.
func RunMigrations(db *DB) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	// golang-migrate's pgx/v5 driver requires a *sql.DB; bridge from the pool.
	sqlDB := stdlib.OpenDBFromPool(db.Pool)

	driver, err := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{})
	if err != nil {
		sqlDB.Close()
		return fmt.Errorf("create migrate driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	// m.Close() closes the driver's held sql.Conn and the sqlDB, releasing all
	// pgxpool slots before pool.Close() is called by the caller.
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	if version, _, err := m.Version(); err == nil {
		fmt.Printf("migrations: at version %d\n", version)
	}

	return nil
}
