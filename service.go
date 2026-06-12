package coresql

import (
	"context"
	"database/sql"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/extra/bunotel"
	"github.com/uptrace/bun/schema"
	"go.uber.org/fx"
)

const defaultSlowQuery = time.Second

// NewService opens a bun.DB, configures the connection pool, and registers
// fx lifecycle hooks for Ping (OnStart) and Close (OnStop).
//
// The dialect must match the chosen driver; import one of:
//   - github.com/uptrace/bun/dialect/pgdialect    → pgdialect.New()
//   - github.com/uptrace/bun/dialect/mysqldialect → mysqldialect.New()
//   - github.com/uptrace/bun/dialect/sqlitedialect → sqlitedialect.New()
func NewService(config *Config, dialect schema.Dialect, lc fx.Lifecycle) (*bun.DB, error) {
	sqldb, err := sql.Open(config.Driver, config.DSN)
	if err != nil {
		return nil, err
	}
	if config.MaxOpen > 0 {
		sqldb.SetMaxOpenConns(config.MaxOpen)
	}
	if config.MaxIdle > 0 {
		sqldb.SetMaxIdleConns(config.MaxIdle)
	}
	if config.MaxLifetime > 0 {
		sqldb.SetConnMaxLifetime(config.MaxLifetime)
	}

	slowDuration := config.SlowQuery
	if slowDuration == 0 {
		slowDuration = defaultSlowQuery
	}

	db := bun.NewDB(sqldb, dialect).WithQueryHook(
		bunotel.NewQueryHook(bunotel.WithFormattedQueries(true)),
	)
	db.WithQueryHook(&queryLogger{slowDuration: slowDuration})

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return db.PingContext(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return db.Close()
		},
	})
	return db, nil
}
