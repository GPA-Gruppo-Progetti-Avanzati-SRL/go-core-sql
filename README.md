# GO-CORE-SQL

## Installation

    go get github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-sql

---

Astrazione SQL per GPA basata su [`uptrace/bun`](https://bun.uptrace.dev/). Fornisce CRUD generici tipizzati, filter builder da struct, paginazione, transazioni e integrazione con OpenTelemetry e zerolog. Dialect-agnostic: PostgreSQL, MySQL, SQLite.

## Funzionalità principali

### IRecord — modello tabella

Implementare `IRecord` nel proprio struct aggiungendo `bun.BaseModel` per il nome tabella e i tag `bun:` per colonne e chiave primaria:

```go
type Person struct {
    bun.BaseModel `bun:"table:people"`

    ID      string `bun:"id,pk"   json:"id"`
    Nome    string `bun:"nome"    json:"nome"`
    Status  string `bun:"status"  json:"status"`
}

// Value receiver — obbligatorio
func (p Person) GetTableName(ctx context.Context) string { return "people" }
```

### IFilter — filter builder

Struct con tag `col:` (nome colonna SQL), `op:` (operatore) e `omitempty:"true"` (salta il campo se zero-value):

```go
type PersonFilter struct {
    ID     string   `col:"id"     op:"="    omitempty:"true"`
    Status string   `col:"status" op:"="    omitempty:"true"`
    IDs    []string `col:"id"     op:"IN"   omitempty:"true"`
    Nome   string   `col:"nome"   op:"STARTSWITH" omitempty:"true"`
}

func (f PersonFilter) GetFilterTableName(ctx context.Context) string { return "people" }
```

**Operatori supportati:** `=`, `!=`, `>`, `>=`, `<`, `<=`, `IN`, `NOT IN`, `LIKE`, `ILIKE`, `STARTSWITH`, `ENDSWITH`, `CONTAINS`, `IS NULL`, `IS NOT NULL`.

### CRUD generici

```go
// Lettura
item,  appErr := coresql.GetById[T](ctx, db, id)
item,  appErr := coresql.GetByFilter[T](ctx, db, filter)
items, appErr := coresql.GetAllByFilter[T](ctx, db, filter)
items, appErr := coresql.GetAllByFilterSorted[T](ctx, db, filter, sort)
items, appErr := coresql.GetPageByFilter[T](ctx, db, filter, paging)
count, appErr := coresql.CountRows(ctx, db, filter)

// Scrittura
appErr := coresql.InsertOne(ctx, db, &obj)
appErr := coresql.InsertMany[T](ctx, db, objs)
appErr := coresql.UpdateOne(ctx, db, filter, map[string]any{"col": val})
appErr := coresql.UpdateMany(ctx, db, filter, map[string]any{"col": val}, n)
appErr := coresql.DeleteOne(ctx, db, filter)
appErr := coresql.DeleteMany(ctx, db, filter)

// PostgreSQL
id, appErr := coresql.NextSequenceValue(ctx, db, "my_seq")
```

`db` è `bun.IDB` — funziona sia con `*bun.DB` che con `bun.Tx`.

### Transazioni

```go
appErr := coresql.ExecTransaction(ctx, db, func(ctx context.Context, tx bun.IDB) error {
    if appErr := coresql.InsertOne(ctx, tx, &order); appErr != nil {
        return appErr
    }
    return coresql.UpdateOne(ctx, tx, stockFilter, map[string]any{"qty": newQty})
})
```

`ExecTransaction` richiede `*bun.DB` (non `bun.IDB`).

### Paginazione e sort

```go
paging := page.InitPaging(nil, pageSize, pageNum, 0)
items, appErr := coresql.GetPageByFilter[T](ctx, db, filter, paging)
// paging.TotalCount popolato automaticamente

sort := page.ParseSort("created_at:desc,nome:asc")
items, appErr := coresql.GetAllByFilterSorted[T](ctx, db, filter, sort)
```

### Query logging

Il query logger è **sempre attivo** — il livello zerolog controlla cosa viene emesso:

| Condizione | Livello zerolog |
|------------|----------------|
| Errore SQL (non `ErrNoRows`) | `Error` |
| Query più lenta di `slowQuery` | `Warn` |
| Tutte le altre query | `Trace` |

In sviluppo impostare `LOG_LEVEL=trace` per vedere tutte le query. In produzione (`info` o superiore) vengono loggati solo errori e slow query.

## Setup e configurazione

**`services/services.go`** (esempio PostgreSQL):

```go
core.Supply(&cfg.SQL)
core.ProvideAs[schema.Dialect](pgdialect.New)
core.Provide(coresql.NewService)
```

**`app-config.go`** — blank import del driver pgx:

```go
import _ "github.com/jackc/pgx/v5/stdlib"
```

**Config YAML:**

```yaml
sql:
  driver: "pgx"
  dsn: "postgres://${PG_USER}:${PG_PWD}@localhost:5432/mydb?sslmode=disable"
  maxOpen: 20
  maxIdle: 5
  maxLifetime: 10m
  slowQuery: 500ms   # default 1s se omesso
```

**Dialetti disponibili:**

| Dialect | Import | Costruttore |
|---------|--------|-------------|
| PostgreSQL | `bun/dialect/pgdialect` + `_ "github.com/jackc/pgx/v5/stdlib"` | `pgdialect.New()` |
| MySQL | `bun/dialect/mysqldialect` + driver MySQL | `mysqldialect.New()` |
| SQLite | `bun/dialect/sqlitedialect` + `_ "github.com/mattn/go-sqlite3"` | `sqlitedialect.New()` |
