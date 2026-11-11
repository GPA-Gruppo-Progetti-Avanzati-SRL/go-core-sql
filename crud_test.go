package coresql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app"
	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app/page"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/feature"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/schema"
)

// I test dei CRUD girano su uno stub driver database/sql che registra le query e
// non tocca nessun database: così si verificano il routing su Service.idb (db vs
// tx) e l'SQL generato senza aggiungere a go-core-sql un dialect concreto o un
// driver, che restano dipendenze dell'applicazione (vedi dialect_test.go).

type loggedQuery struct {
	sql  string
	inTx bool
}

type queryLog struct {
	mu        sync.Mutex
	queries   []loggedQuery
	begins    int
	commits   int
	rollbacks int
}

func (l *queryLog) record(q string, inTx bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.queries = append(l.queries, loggedQuery{sql: q, inTx: inTx})
}

// last restituisce l'ultima query registrata.
func (l *queryLog) last() loggedQuery {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.queries) == 0 {
		return loggedQuery{}
	}
	return l.queries[len(l.queries)-1]
}

type stubDriver struct{ log *queryLog }

func (d *stubDriver) Open(string) (driver.Conn, error) { return &stubConn{log: d.log}, nil }

type stubConn struct {
	log  *queryLog
	inTx bool
}

func (c *stubConn) Prepare(query string) (driver.Stmt, error) {
	return &stubStmt{conn: c, query: query}, nil
}
func (c *stubConn) Close() error { return nil }

func (c *stubConn) Begin() (driver.Tx, error) {
	c.log.mu.Lock()
	c.log.begins++
	c.log.mu.Unlock()
	c.inTx = true
	return &stubTx{conn: c}, nil
}

func (c *stubConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

func (c *stubConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.log.record(query, c.inTx)
	return stubResult{}, nil
}

func (c *stubConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.log.record(query, c.inTx)
	return newStubRows(query), nil
}

type stubTx struct{ conn *stubConn }

func (t *stubTx) Commit() error {
	t.conn.log.mu.Lock()
	t.conn.log.commits++
	t.conn.log.mu.Unlock()
	t.conn.inTx = false
	return nil
}

func (t *stubTx) Rollback() error {
	t.conn.log.mu.Lock()
	t.conn.log.rollbacks++
	t.conn.log.mu.Unlock()
	t.conn.inTx = false
	return nil
}

type stubStmt struct {
	conn  *stubConn
	query string
}

func (s *stubStmt) Close() error  { return nil }
func (s *stubStmt) NumInput() int { return -1 }
func (s *stubStmt) Exec([]driver.Value) (driver.Result, error) {
	s.conn.log.record(s.query, s.conn.inTx)
	return stubResult{}, nil
}
func (s *stubStmt) Query([]driver.Value) (driver.Rows, error) {
	s.conn.log.record(s.query, s.conn.inTx)
	return newStubRows(s.query), nil
}

type stubResult struct{}

func (stubResult) LastInsertId() (int64, error) { return 1, nil }
func (stubResult) RowsAffected() (int64, error) { return 1, nil }

// stubRows non restituisce righe, tranne per le COUNT(*) — dove un risultato
// vuoto farebbe fallire lo Scan di bun con sql.ErrNoRows.
type stubRows struct {
	cols []string
	rows [][]driver.Value
	pos  int
}

func newStubRows(query string) *stubRows {
	if strings.Contains(strings.ToLower(query), "count(") {
		return &stubRows{cols: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}
	}
	return &stubRows{cols: []string{"id"}}
}

func (r *stubRows) Columns() []string { return r.cols }
func (r *stubRows) Close() error      { return nil }
func (r *stubRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

// stubDialect è un dialect minimo ma completo: a differenza del fakeDialect di
// dialect_test.go (che serve solo alla logica di feature-flag) restituisce un
// *schema.Tables reale, necessario a bun per costruire query su modello.
type stubDialect struct {
	schema.BaseDialect
	tables *schema.Tables
	name   dialect.Name // Invalid (zero) = SQLite
}

func newStubDialect() *stubDialect {
	d := &stubDialect{}
	d.tables = schema.NewTables(d)
	return d
}

func (d *stubDialect) Init(*sql.DB) {}
func (d *stubDialect) Name() dialect.Name {
	if d.name == dialect.Invalid {
		return dialect.SQLite
	}
	return d.name
}
func (d *stubDialect) Features() feature.Feature                                  { return feature.Returning }
func (d *stubDialect) Tables() *schema.Tables                                     { return d.tables }
func (d *stubDialect) OnTable(*schema.Table)                                      {}
func (d *stubDialect) IdentQuote() byte                                           { return '"' }
func (d *stubDialect) AppendSequence([]byte, *schema.Table, *schema.Field) []byte { return nil }
func (d *stubDialect) DefaultVarcharLen() int                                     { return 0 }
func (d *stubDialect) DefaultSchema() string                                      { return "" }

// testRecord è il record usato dai test: il nome tabella arriva da IRecord.
type testRecord struct {
	bun.BaseModel `bun:"table:test_records"`

	Id     string `bun:"id,pk"`
	Status string `bun:"status"`
}

func (testRecord) GetTableName(ctx context.Context) string { return "test_records" }

type testFilter struct {
	Status string `col:"status" op:"=" omitempty:"true"`
}

func (testFilter) GetFilterTableName(ctx context.Context) string { return "test_records" }

// stubDriverSeq rende unico il nome del driver a ogni registrazione. Derivarlo dal solo t.Name()
// bastava con -count=1, ma database/sql.Register PANICA sul nome ripetuto e il registro dei driver
// è globale al processo: con -count=2 lo stesso test gira due volte e la seconda esplodeva. È un
// problema di questo helper, non del registry DI di go-core-app.
var stubDriverSeq atomic.Int64

// newTestService costruisce un Service sullo stub driver, senza lifecycle fx.
func newTestService(t *testing.T) (*Service, *queryLog) {
	t.Helper()
	return newTestServiceDialect(t, dialect.Invalid)
}

// newTestServiceDialect è newTestService con il nome di dialetto indicato, per i rami che
// dipendono da dialect.Name (oggi: Oracle).
func newTestServiceDialect(t *testing.T, dn dialect.Name) (*Service, *queryLog) {
	t.Helper()
	log := &queryLog{}
	name := "coresql-stub-" + t.Name() + "-" + strconv.FormatInt(stubDriverSeq.Add(1), 10)
	sql.Register(name, &stubDriver{log: log})
	sqldb, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	d := newStubDialect()
	d.name = dn
	db := bun.NewDB(sqldb, d)
	return &Service{db: db, idb: db}, log
}

func TestServiceGetByIdNotFound(t *testing.T) {
	s, log := newTestService(t)

	_, appErr := s.GetById[testRecord](t.Context(), "abc")
	if appErr == nil {
		t.Fatal("atteso NotFoundError con zero righe")
	}
	if appErr.StatusCode != 404 {
		t.Fatalf("atteso 404, ottenuto %d (%s)", appErr.StatusCode, appErr.Message)
	}
	// La causa del driver resta raggiungibile: un chiamante che riceve solo
	// l'*core.Error può ancora distinguere "nessuna riga" da un 404 sintetico.
	if !errors.Is(appErr, sql.ErrNoRows) {
		t.Error("NotFoundError deve conservare sql.ErrNoRows come causa")
	}
	got := log.last().sql
	if !strings.Contains(got, "test_records") || !strings.Contains(got, "id = ") {
		t.Fatalf("SQL inatteso: %s", got)
	}
}

func TestServiceGetAllByFilterUsesFilterTags(t *testing.T) {
	s, log := newTestService(t)

	if _, appErr := s.GetAllByFilter[testRecord](t.Context(), testFilter{Status: "PENDING"}); appErr != nil {
		t.Fatalf("GetAllByFilter: %v", appErr.Message)
	}
	got := log.last().sql
	if !strings.Contains(got, "status = ") {
		t.Fatalf("la WHERE del filtro non è arrivata nella query: %s", got)
	}
}

func TestServiceInsertOne(t *testing.T) {
	s, log := newTestService(t)

	if appErr := s.InsertOne(t.Context(), &testRecord{Id: "abc", Status: "PENDING"}); appErr != nil {
		t.Fatalf("InsertOne: %v", appErr.Message)
	}
	got := log.last()
	if !strings.HasPrefix(strings.ToUpper(got.sql), "INSERT") {
		t.Fatalf("attesa una INSERT, ottenuto: %s", got.sql)
	}
	if got.inTx {
		t.Fatal("la INSERT fuori da ExecTransaction non deve girare in transazione")
	}
}

// TestServiceExecTransactionCommit verifica il punto centrale del pattern: il
// *Service passato a fn instrada le query sulla transazione, non sul *bun.DB.
func TestServiceExecTransactionCommit(t *testing.T) {
	s, log := newTestService(t)

	appErr := s.ExecTransaction(t.Context(), func(ctx context.Context, tx *Service) error {
		// Attenzione: *core.Error va convertito con un if, non ritornato
		// direttamente, altrimenti un nil tipizzato diventa un error non-nil.
		if err := tx.InsertOne(ctx, &testRecord{Id: "abc"}); err != nil {
			return err
		}
		return nil
	})
	if appErr != nil {
		t.Fatalf("ExecTransaction: %v", appErr.Message)
	}
	got := log.last()
	if !got.inTx {
		t.Fatalf("la query dentro ExecTransaction non è girata sulla tx: %s", got.sql)
	}
	if log.begins != 1 || log.commits != 1 || log.rollbacks != 0 {
		t.Fatalf("attesi 1 begin / 1 commit / 0 rollback, ottenuti %d/%d/%d", log.begins, log.commits, log.rollbacks)
	}
}

func TestServiceExecTransactionRollback(t *testing.T) {
	s, log := newTestService(t)

	boom := errors.New("boom")
	appErr := s.ExecTransaction(t.Context(), func(ctx context.Context, tx *Service) error {
		if err := tx.InsertOne(ctx, &testRecord{Id: "abc"}); err != nil {
			return err
		}
		return boom
	})
	if appErr == nil {
		t.Fatal("atteso errore propagato da fn")
	}
	if log.commits != 0 || log.rollbacks != 1 {
		t.Fatalf("atteso rollback, ottenuti %d commit / %d rollback", log.commits, log.rollbacks)
	}
}

// Un ExecTransaction dentro un altro partecipa alla transazione del chiamante: una sola begin, un
// solo commit, e le query dell'interno girano sulla stessa tx. Prima ne apriva una seconda sul
// *bun.DB radice.
func TestServiceExecTransactionAnnidataPartecipa(t *testing.T) {
	s, log := newTestService(t)

	appErr := s.ExecTransaction(t.Context(), func(ctx context.Context, tx *Service) error {
		if err := tx.InsertOne(ctx, &testRecord{Id: "esterna"}); err != nil {
			return err
		}
		if err := tx.ExecTransaction(ctx, func(ctx context.Context, inner *Service) error {
			if inner != tx {
				t.Error("l'interno deve ricevere il Service della transazione esterna")
			}
			if err := inner.InsertOne(ctx, &testRecord{Id: "interna"}); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
		return nil
	})
	if appErr != nil {
		t.Fatalf("ExecTransaction: %v", appErr.Message)
	}
	if log.begins != 1 || log.commits != 1 || log.rollbacks != 0 {
		t.Fatalf("attesi 1 begin / 1 commit / 0 rollback, ottenuti %d/%d/%d", log.begins, log.commits, log.rollbacks)
	}
	if !log.last().inTx {
		t.Fatal("la query dell'interno non e' girata sulla tx")
	}
}

// Un errore dell'interno propagato dal chiamante annulla l'intera transazione, e una sola.
func TestServiceExecTransactionAnnidataErroreAnnullaTutto(t *testing.T) {
	s, log := newTestService(t)

	boom := errors.New("boom")
	appErr := s.ExecTransaction(t.Context(), func(ctx context.Context, tx *Service) error {
		if err := tx.InsertOne(ctx, &testRecord{Id: "esterna"}); err != nil {
			return err
		}
		if err := tx.ExecTransaction(ctx, func(ctx context.Context, inner *Service) error {
			if err := inner.InsertOne(ctx, &testRecord{Id: "interna"}); err != nil {
				return err
			}
			return boom
		}); err != nil {
			return err
		}
		return nil
	})
	if appErr == nil || !errors.Is(appErr, boom) {
		t.Fatalf("atteso l'errore dell'interno, ottenuto %v", appErr)
	}
	if log.begins != 1 || log.commits != 0 || log.rollbacks != 1 {
		t.Fatalf("attesi 1 begin / 0 commit / 1 rollback, ottenuti %d/%d/%d", log.begins, log.commits, log.rollbacks)
	}
}

// Su una sola connessione una transazione annidata aperta sul DB radice resterebbe in attesa per
// sempre: la prova che l'interno usa davvero la transazione del chiamante.
func TestServiceExecTransactionAnnidataStessaConnessione(t *testing.T) {
	s := sqliteService(t) // un'unica connessione
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	boom := errors.New("boom")
	appErr := s.ExecTransaction(ctx, func(ctx context.Context, tx *Service) error {
		if err := tx.InsertOne(ctx, &persona{Id: "p1", Nome: "Ada"}); err != nil {
			return err
		}
		if err := tx.ExecTransaction(ctx, func(ctx context.Context, inner *Service) error {
			if err := inner.InsertOne(ctx, &persona{Id: "p2", Nome: "Bo"}); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
		return boom // l'esterna annulla anche cio' che l'interna ha scritto
	})
	if appErr == nil || !errors.Is(appErr, boom) {
		// Con una transazione annidata sul DB radice l'errore sarebbe il timeout del context.
		t.Fatalf("atteso l'errore dell'esterna, ottenuto %v", appErr)
	}
	n, err := s.CountRows(t.Context(), tutteLePersone{})
	if err != nil {
		t.Fatalf("CountRows: %v", err)
	}
	if n != 0 {
		t.Fatalf("dopo il rollback dell'esterna restano %d righe: la transazione interna era indipendente", n)
	}
}

// TestScrittureConFiltroVuoto: un filtro coi campi omitempty tutti vuoti — query param assenti —
// non produce condizioni. Per una lettura è "tutto", per una scrittura è un errore: prima
// UpdateMany/DeleteMany toccavano l'intera tabella e DeleteOne una riga qualsiasi.
func TestScrittureConFiltroVuoto(t *testing.T) {
	s, log := newTestService(t)
	ctx := t.Context()
	empty := testFilter{}
	for name, call := range map[string]func() *core.Error{
		"UpdateOne":  func() *core.Error { return s.UpdateOne(ctx, empty, map[string]any{"status": "x"}) },
		"UpdateMany": func() *core.Error { return s.UpdateMany(ctx, empty, map[string]any{"status": "x"}, 1) },
		"DeleteOne":  func() *core.Error { return s.DeleteOne(ctx, empty) },
		"DeleteMany": func() *core.Error { return s.DeleteMany(ctx, empty) },
	} {
		before := len(log.queries)
		appErr := call()
		if appErr == nil || appErr.Code != CodeEmptyFilter || appErr.StatusCode != 422 {
			t.Errorf("%s: atteso %s (422), ottenuto %v", name, CodeEmptyFilter, appErr)
		}
		if len(log.queries) != before {
			t.Errorf("%s: la query è partita comunque: %v", name, log.queries[before:])
		}
	}
}

type persona struct {
	bun.BaseModel `bun:"table:persone"`
	Id            string `bun:"id,pk"`
	Nome          string `bun:"nome"`
}

func (persona) GetTableName(context.Context) string { return "persone" }

type personaPerId struct {
	Id string `col:"id" op:"=" omitempty:"true"`
}

func (personaPerId) GetFilterTableName(context.Context) string { return "persone" }

type tutteLePersone struct {
	Nome string `col:"nome" op:"=" omitempty:"true"`
}

func (tutteLePersone) GetFilterTableName(context.Context) string { return "persone" }

func sqliteService(t *testing.T) *Service {
	t.Helper()
	sqldb, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	sqldb.SetMaxOpenConns(1) // :memory: è per connessione
	t.Cleanup(func() {
		if err := sqldb.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	db := bun.NewDB(sqldb, sqlitedialect.New())
	if _, err := db.NewCreateTable().Model((*persona)(nil)).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	return &Service{db: db, idb: db}
}

// Nessuna riga aggiornata è un 404, non un 500: l'id non esiste. Un update che riscrive gli stessi
// valori resta un successo (SQLite e PostgreSQL contano le righe trovate).
func TestUpdateOne_NonTrovatoEIdempotente(t *testing.T) {
	s := sqliteService(t)
	ctx := t.Context()
	if err := s.InsertOne(ctx, &persona{Id: "p1", Nome: "Ada"}); err != nil {
		t.Fatal(err.Message)
	}
	for i := range 2 {
		if err := s.UpdateOne(ctx, personaPerId{Id: "p1"}, map[string]any{"nome": "Ada"}); err != nil {
			t.Fatalf("UpdateOne #%d: %v", i+1, err)
		}
	}
	if err := s.UpdateOne(ctx, personaPerId{Id: "nessuno"}, map[string]any{"nome": "x"}); err == nil || err.StatusCode != 404 {
		t.Fatalf("UpdateOne su una riga assente: %v, atteso 404", err)
	}
}

// Le pagine sono ordinate per chiave primaria: senza ORDER BY due pagine possono ripetere o saltare
// righe.
func TestGetPageByFilter_OrdinePerChiave(t *testing.T) {
	s := sqliteService(t)
	ctx := t.Context()
	for _, id := range []string{"e", "b", "d", "a", "c"} {
		if err := s.InsertOne(ctx, &persona{Id: id, Nome: "x"}); err != nil {
			t.Fatal(err.Message)
		}
	}
	var visti []string
	for pagina := 1; pagina <= 3; pagina++ {
		out, err := s.GetPageByFilter[persona](ctx, tutteLePersone{}, &page.Paging{CurrentPage: pagina, PageSize: 2})
		if err != nil {
			t.Fatalf("pagina %d: %v", pagina, err)
		}
		for _, o := range out {
			visti = append(visti, o.Id)
		}
	}
	if strings.Join(visti, "") != "abcde" {
		t.Fatalf("righe per pagina = %v, attese a..e in ordine", visti)
	}
}

// Oracle fino alla 19c rifiuta l'INSERT multi-riga che bun emette per una slice: InsertMany deve
// fare un INSERT per riga, e tutti dentro una transazione, perché resti atomica come altrove.
func TestServiceInsertMany_OracleUnaRigaPerInsertInTransazione(t *testing.T) {
	s, log := newTestServiceDialect(t, dialect.Oracle)

	objs := []*testRecord{{Id: "a"}, {Id: "b"}, {Id: "c"}}
	if appErr := s.InsertMany[testRecord](t.Context(), objs); appErr != nil {
		t.Fatalf("InsertMany: %v", appErr.Message)
	}
	var inserts []loggedQuery
	for _, q := range log.queries {
		if strings.HasPrefix(strings.ToUpper(q.sql), "INSERT") {
			inserts = append(inserts, q)
		}
	}
	if len(inserts) != len(objs) {
		t.Fatalf("attese %d INSERT, ottenute %d: %v", len(objs), len(inserts), inserts)
	}
	for _, q := range inserts {
		if !q.inTx {
			t.Errorf("INSERT fuori transazione: %s", q.sql)
		}
		if strings.Count(q.sql, "VALUES (") != 1 || strings.Contains(q.sql, "), (") {
			t.Errorf("INSERT multi-riga su Oracle: %s", q.sql)
		}
	}
	if log.begins != 1 || log.commits != 1 {
		t.Errorf("attesa una transazione (begin=%d commit=%d)", log.begins, log.commits)
	}
}

// Dentro ExecTransaction InsertMany usa la transazione del chiamante, senza aprirne un'altra
// (bun la anniderebbe con un savepoint, e Oracle non ha RELEASE SAVEPOINT).
func TestServiceInsertMany_OracleDentroExecTransaction(t *testing.T) {
	s, log := newTestServiceDialect(t, dialect.Oracle)

	appErr := s.ExecTransaction(t.Context(), func(ctx context.Context, tx *Service) error {
		if err := tx.InsertMany[testRecord](ctx, []*testRecord{{Id: "a"}, {Id: "b"}}); err != nil {
			return err
		}
		return nil
	})
	if appErr != nil {
		t.Fatalf("ExecTransaction: %v", appErr.Message)
	}
	if log.begins != 1 {
		t.Errorf("attesa una sola transazione, aperte %d", log.begins)
	}
	for _, q := range log.queries {
		if strings.Contains(strings.ToUpper(q.sql), "SAVEPOINT") {
			t.Errorf("savepoint inatteso: %s", q.sql)
		}
	}
}

// Sugli altri dialetti resta l'INSERT multi-riga, fuori transazione.
func TestServiceInsertMany_AltriDialettiUnaSolaInsert(t *testing.T) {
	s, log := newTestService(t)

	if appErr := s.InsertMany[testRecord](t.Context(), []*testRecord{{Id: "a"}, {Id: "b"}}); appErr != nil {
		t.Fatalf("InsertMany: %v", appErr.Message)
	}
	if len(log.queries) != 1 || log.begins != 0 {
		t.Fatalf("attesa una sola INSERT senza transazione, ottenute %d query e %d begin", len(log.queries), log.begins)
	}
}
