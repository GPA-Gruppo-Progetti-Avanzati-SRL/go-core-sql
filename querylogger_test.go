package coresql

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"
)

// Un errore o una query lenta finiscono nei log di produzione: i valori dei parametri no. Prima
// il log portava la query formattata, cioè il segreto in chiaro.
func TestQueryLogger_NonLoggaIParametri(t *testing.T) {
	var buf bytes.Buffer
	orig, origLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = zerolog.New(&buf)
	zerolog.SetGlobalLevel(zerolog.DebugLevel) // sotto Trace: il testo coi valori resta spento
	t.Cleanup(func() { log.Logger = orig; zerolog.SetGlobalLevel(origLevel) })

	sqldb, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqldb.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	db := bun.NewDB(sqldb, sqlitedialect.New()).WithQueryHook(&queryLogger{slowDuration: time.Hour})
	ctx := context.Background()
	const secret = "s3gr3t0-da-non-loggare"

	// Raw: la tabella non esiste, quindi è un errore — loggato a Error.
	if _, err := db.NewRaw("SELECT * FROM utenti WHERE password = ?", secret).Exec(ctx); err == nil {
		t.Fatal("attesa un errore")
	}
	// Costruita con bun: stesso esito.
	if err := db.NewSelect().Table("utenti").Where("password = ?", secret).Scan(ctx, &struct{}{}); err == nil {
		t.Fatal("attesa un errore")
	}

	out := buf.String()
	if strings.Count(out, "bun: query error") != 2 {
		t.Fatalf("attesi due log d'errore, ottenuto:\n%s", out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("il valore del parametro è nel log:\n%s", out)
	}
	if !strings.Contains(out, "password = ?") {
		t.Fatalf("il log deve portare la query coi segnaposto:\n%s", out)
	}
}
