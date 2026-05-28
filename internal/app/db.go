package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"orgstructure/internal/config"
)

// OpenDB establishes a GORM connection with sensible pool defaults and
// retries connecting a handful of times so the API survives Postgres being
// slightly slower to come up than the API container.
func OpenDB(ctx context.Context, cfg config.DBConfig, log *slog.Logger) (*gorm.DB, error) {
	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}

	var (
		db  *gorm.DB
		err error
	)
	const maxAttempts = 30
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		db, err = gorm.Open(postgres.Open(cfg.DSN()), gormCfg)
		if err == nil {
			sqlDB, pingErr := db.DB()
			if pingErr == nil {
				if pingErr = sqlDB.PingContext(ctx); pingErr == nil {
					break
				}
				err = pingErr
			} else {
				err = pingErr
			}
		}

		log.Warn("waiting for database",
			"attempt", attempt,
			"max", maxAttempts,
			"error", err,
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if err != nil {
		return nil, fmt.Errorf("connect to database after %d attempts: %w", maxAttempts, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	return db, nil
}

// RunMigrations applies every SQL file in `dir` using goose.
// It is safe to call on every startup: goose skips migrations that have
// already been recorded in goose_db_version.
func RunMigrations(db *sql.DB, dir string, log *slog.Logger) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose set dialect: %w", err)
	}
	goose.SetLogger(gooseLogger{log: log})
	if err := goose.Up(db, dir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// gooseLogger adapts slog to the goose.Logger interface.
type gooseLogger struct{ log *slog.Logger }

func (g gooseLogger) Fatal(v ...interface{}) { g.log.Error(fmt.Sprint(v...)) }
func (g gooseLogger) Fatalf(format string, v ...interface{}) {
	g.log.Error(fmt.Sprintf(format, v...))
}
func (g gooseLogger) Print(v ...interface{}) { g.log.Info(fmt.Sprint(v...)) }
func (g gooseLogger) Println(v ...interface{}) {
	g.log.Info(fmt.Sprint(v...))
}
func (g gooseLogger) Printf(format string, v ...interface{}) {
	g.log.Info(fmt.Sprintf(format, v...))
}
