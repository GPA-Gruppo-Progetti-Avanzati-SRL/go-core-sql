package coresql

import (
	"context"
	"database/sql"
	"reflect"
	"time"

	core "github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/extra/bunotel"
	"go.uber.org/fx"
)

const defaultSlowQuery = time.Second

// Service è il servizio SQL: possiede il *bun.DB, espone le operazioni CRUD
// generiche (metodi generici, Go 1.27+) e le transazioni. È l'unico handle SQL
// dell'applicazione, come *coremongo.Service lo è per Mongo.
//
// idb è il destinatario effettivo delle query: coincide con db, tranne nel
// Service passato a ExecTransaction, dove è la *bun.Tx. Così gli stessi metodi
// valgono dentro e fuori transazione.
type Service struct {
	db  *bun.DB
	idb bun.IDB
}

// DB espone l'handle bun sottostante, per le query bun native, il DDL e tutto
// ciò che non passa dai CRUD generici (es. locker.EnsureTable).
func (s *Service) DB() *bun.DB { return s.db }

// IDB restituisce il destinatario delle query: la *bun.Tx dentro
// ExecTransaction, il *bun.DB altrimenti. Serve a chi costruisce query bun a
// mano e deve rispettare la transazione in corso.
func (s *Service) IDB() bun.IDB { return s.idb }

// ExecTransaction esegue fn dentro una transazione: rollback su errore, commit
// al successo. fn riceve un *Service legato alla transazione, quindi al suo
// interno si usano gli stessi metodi CRUD.
//
// Chiamata su un *Service già legato a una transazione (cioè dentro la fn di un altro
// ExecTransaction) non ne apre una seconda: partecipa a quella del chiamante, e commit e rollback
// restano suoi. Se fn ritorna un errore, ExecTransaction lo ritorna senza annullare nulla da sé:
// sta al chiamante propagarlo, e il rollback è quello della chiamata più esterna. Non usa
// savepoint, perché Oracle non ha RELEASE SAVEPOINT, che bun emette per una transazione annidata:
// le scritture di una fn fallita restano nella transazione finché l'esterna non fa rollback. È la
// stessa regola di InsertMany su Oracle.
//
// Prima apriva sempre una transazione nuova sul *bun.DB radice: una chiamata annidata girava su
// un'altra connessione, indipendente, e il suo commit non seguiva il rollback dell'esterna.
func (s *Service) ExecTransaction(ctx context.Context, fn func(ctx context.Context, tx *Service) error) *core.Error {
	if s.inTx() {
		if err := fn(ctx, s); err != nil {
			return errs.Tech(CodeTransaction).WithCause(err)
		}
		return nil
	}
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(ctx, &Service{db: s.db, idb: &tx})
	}); err != nil {
		return errs.Tech(CodeTransaction).WithCause(err)
	}
	return nil
}

// singlePK ritorna il nome della colonna della chiave primaria del modello, o "" se il modello non
// ne ha una sola (nessuna, o composta).
func (s *Service) singlePK(typ reflect.Type) string {
	if typ.Kind() != reflect.Struct {
		return ""
	}
	pks := s.db.Table(typ).PKs
	if len(pks) != 1 {
		return ""
	}
	return pks[0].Name
}

// inTx dice se il Service è legato a una transazione: idb è la *bun.Tx e non il *bun.DB.
func (s *Service) inTx() bool { return s.idb != bun.IDB(s.db) }

// serviceParams raccoglie le dipendenze di newService. Il dialect arriva da
// dialectSource, supplita da Module: è una scelta compile-time dell'app (quale
// driver/dialect importare), non un valore di configurazione.
type serviceParams struct {
	core.In
	Config    *Config
	Dialect   dialectSource
	Lifecycle fx.Lifecycle
}

// newService apre il *bun.DB, configura il pool e registra gli hook fx di
// lifecycle (Ping su OnStart, Close su OnStop).
//
// Con un dialect Oracle il workaround OffsetFetch è applicato automaticamente
// (vedi applyDialectWorkarounds), così le query con LIMIT/OFFSET generano SQL
// valido senza wrapping al call site.
func newService(p serviceParams) (*Service, error) {
	sqldb, err := sql.Open(p.Config.Driver, p.Config.DSN)
	if err != nil {
		return nil, err
	}
	if p.Config.MaxOpen > 0 {
		sqldb.SetMaxOpenConns(p.Config.MaxOpen)
	}
	if p.Config.MaxIdle > 0 {
		sqldb.SetMaxIdleConns(p.Config.MaxIdle)
	}
	if p.Config.MaxLifetime > 0 {
		sqldb.SetConnMaxLifetime(p.Config.MaxLifetime)
	}

	slowDuration := p.Config.SlowQuery
	if slowDuration == 0 {
		slowDuration = defaultSlowQuery
	}

	// Negli span va il testo della query con i segnaposto, non con i valori: una query formattata
	// porta dentro i parametri (email, codici fiscali, hash di password), e il backend di tracing li
	// conserverebbe con una retention e un controllo d'accesso che non sono quelli del database.
	db := bun.NewDB(sqldb, applyDialectWorkarounds(p.Dialect.dialect)).WithQueryHook(
		bunotel.NewQueryHook(bunotel.WithFormattedQueries(false)),
	).WithQueryHook(&queryLogger{slowDuration: slowDuration})

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return db.PingContext(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return db.Close()
		},
	})
	return &Service{db: db, idb: db}, nil
}
