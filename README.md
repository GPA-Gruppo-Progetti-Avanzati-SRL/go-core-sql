# GO-CORE-SQL

## Installation

    go get github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-sql

---

Astrazione SQL per GPA basata su [`uptrace/bun`](https://bun.uptrace.dev/). Fornisce CRUD generici tipizzati, filter builder da struct, paginazione, transazioni e integrazione con OpenTelemetry e zerolog. Dialect-agnostic: PostgreSQL, MySQL, SQLite.

**Richiede Go 1.27+**: i CRUD generici sono metodi di `*coresql.Service` (i metodi generici sono una feature Go 1.27).

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

### CRUD generici — metodi del Service

I CRUD sono **metodi generici di `*coresql.Service`** (richiedono Go 1.27+), come in
go-core-mongo: il Service è l'unico handle SQL dell'applicazione e viene iniettato da fx.

```go
// Lettura
item,  appErr := s.GetById[T](ctx, id)
item,  appErr := s.GetByFilter[T](ctx, filter)
items, appErr := s.GetAllByFilter[T](ctx, filter)
items, appErr := s.GetAllByFilterSorted[T](ctx, filter, sort)
items, appErr := s.GetPageByFilter[T](ctx, filter, paging)
count, appErr := s.CountRows(ctx, filter)

// Scrittura
appErr := s.InsertOne(ctx, &obj)
appErr := s.InsertMany[T](ctx, objs)
appErr := s.UpdateOne(ctx, filter, map[string]any{"col": val})
appErr := s.UpdateMany(ctx, filter, map[string]any{"col": val}, n)
appErr := s.DeleteOne(ctx, filter)
appErr := s.DeleteMany(ctx, filter)

// PostgreSQL
id, appErr := s.NextSequenceValue(ctx, "my_seq")
```

Per le query bun native e il DDL: `s.DB()` restituisce il `*bun.DB`, `s.IDB()` il
destinatario corrente (la `*bun.Tx` dentro `ExecTransaction`).

> **Nota sul linguaggio:** un metodo generico non può implementare un metodo di
> interfaccia, quindi `*Service` non è assegnabile a un'interfaccia che dichiari
> questi CRUD. Il data layer dell'app espone i propri metodi concreti (`IData`) e
> richiama al loro interno i metodi del Service.

### Transazioni

```go
appErr := s.ExecTransaction(ctx, func(ctx context.Context, tx *coresql.Service) error {
    if appErr := tx.InsertOne(ctx, &order); appErr != nil {
        return appErr
    }
    return tx.UpdateOne(ctx, stockFilter, map[string]any{"qty": newQty})
})
```

Il `*Service` passato a `fn` instrada le query sulla transazione, quindi dentro `fn`
si usano gli stessi metodi. Attenzione al `*ApplicationError` nil tipizzato: va
convertito con un `if err != nil { return err }` quando `fn` deve ritornare `nil`.

### Paginazione e sort

```go
paging := page.InitPaging(nil, pageSize, pageNum, 0)
items, appErr := s.GetPageByFilter[T](ctx, filter, paging)
// paging.TotalCount popolato automaticamente

sort := page.ParseSort("created_at:desc,nome:asc")
items, appErr := s.GetAllByFilterSorted[T](ctx, filter, sort)
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
// Entry-point unico: supplisce la Config e fornisce *coresql.Service (+ il *bun.DB
// sottostante, per locker/DDL/query bun native).
coresql.Module(&cfg.SQL, pgdialect.New())

// Opzionale: gating per core.Mode
coresql.Module(&cfg.SQL, pgdialect.New(), coresql.WithModes(engine.Api, engine.Batch))
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
