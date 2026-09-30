package coresql

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"
)

type queryLogger struct {
	slowDuration time.Duration
}

// I log a Error e Warn portano la query coi SEGNAPOSTO (queryTemplate), non coi valori: un errore o
// una query lenta finiscono nello stream dei log di produzione, e una query formattata ci porterebbe
// i parametri — dati personali, hash di password. Il testo coi valori resta al solo livello Trace,
// che è un'accensione esplicita di chi sta facendo debug.
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
			Str("query", queryTemplate(event)).
			Dur("dur", dur).
			Msg("bun: query error")
		return
	}

	if dur >= l.slowDuration {
		log.Warn().
			Str("query", queryTemplate(event)).
			Dur("dur", dur).
			Msg("bun: slow query")
		return
	}

}

// queryTemplate è il testo della query con i segnaposto al posto dei valori. Per le query costruite
// con bun lo rigenera con un QueryGen che non interpola (è ciò che fa bunotel); per quelle raw,
// QueryTemplate è già il testo scritto dal chiamante.
func queryTemplate(event *bun.QueryEvent) string {
	if event.IQuery != nil {
		if b, err := event.IQuery.AppendQuery(schema.NewNopQueryGen(), nil); err == nil {
			return string(b)
		}
	}
	return event.QueryTemplate
}
