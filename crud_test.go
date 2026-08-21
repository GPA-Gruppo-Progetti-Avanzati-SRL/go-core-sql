package coresql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/feature"
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
}

func newStubDialect() *stubDialect {
	d := &stubDialect{}
	d.tables = schema.NewTables(d)
	return d
}

func (d *stubDialect) Init(*sql.DB)                                               {}
func (d *stubDialect) Name() dialect.Name                                         { return dialect.SQLite }
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

// newTestService costruisce un Service sullo stub driver, senza lifecycle fx.
func newTestService(t *testing.T) (*Service, *queryLog) {
	t.Helper()
	log := &queryLog{}
	name := "coresql-stub-" + t.Name()
	sql.Register(name, &stubDriver{log: log})
	sqldb, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	db := bun.NewDB(sqldb, newStubDialect())
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
		// Attenzione: *ApplicationError va convertito con un if, non ritornato
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
