package coresql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/uptrace/bun"
)

type queryLogger struct {
	slowDuration time.Duration
}

func (l *queryLogger) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	log.Trace().
		Str("query", event.Query).
		Msg("bun: query")
	return ctx
}

func (l *queryLogger) AfterQuery(_ context.Context, event *bun.QueryEvent) {
	dur := time.Since(event.StartTime)

	if event.Err != nil && !errors.Is(event.Err, sql.ErrNoRows) {
		log.Error().
			Err(event.Err).
			Str("query", event.Query).
			Dur("dur", dur).
			Msg("bun: query error")
		return
	}

	if dur >= l.slowDuration {
		log.Warn().
			Str("query", event.Query).
			Dur("dur", dur).
			Msg("bun: slow query")
		return
	}

}
