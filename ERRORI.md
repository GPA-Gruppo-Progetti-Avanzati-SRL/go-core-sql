# Codici di errore — go-core-sql

I metodi generici di `*coresql.Service` ritornano `*core.ApplicationError`. L'errore di bun /
`database/sql` è sempre allegato come causa: un `sql.ErrNoRows` resta raggiungibile con
`errors.Is` anche dentro il 404.

> **`Ambit` = `go-core-sql`** (costante `coresql.Ambit`) su ogni errore del modulo: è il campo
> che dice da quale libreria viene il guasto. I codici sono costanti esportate in `errors.go` e
> passano tutti dal costruttore `techErr(code)` / `notFound()`.

## Codici emessi

| Codice | HTTP | Costante | Origine | Significato |
|---|---|---|---|---|
| `SQL-FILTER` | 500 | `CodeFilter` | `crud.go:48,68,85,133,160,188,214,231,249` | `buildWhere` fallita: tag `col:`/`op:` non validi. Non è un errore del database: la query non è mai partita |
| `SQL-SELECT` | 500 | `CodeSelect` | `crud.go:37,58,75,95,273` | `SELECT`/`Scan` fallita |
| `SQL-COUNT` | 500 | `CodeCount` | `crud.go:238,258` | `Count` fallita |
| `SQL-INSERT` | 500 | `CodeInsert` | `crud.go:107,122` | `INSERT` fallita (violazione di vincolo compresa: il messaggio del driver è la causa) |
| `SQL-UPDATE` | 500 | `CodeUpdate` | `crud.go:145,172` | `UPDATE` fallita |
| `SQL-DELETE` | 500 | `CodeDelete` | `crud.go:196,221` | `DELETE` fallita |
| `SQL-TX` | 500 | `CodeTransaction` | `service.go:44` | `RunInTx` fallita, o rollback provocato dal callback |
| `SQL-SEQ` | 500 | `CodeSequence` | `crud.go:282` | `nextval` fallita |
| `SQL-EMPTY-SET` | 500 | `CodeEmptySet` | `crud.go:136,163` | nessun campo da aggiornare: la clausola `SET` sarebbe vuota. Errore di costruzione della query, non del DB |
| `SQL-UPDATE-INC` | 500 | `CodeUpdateInc` | `crud.go:150,177` | **update incoerente**: righe aggiornate diverse dalle attese (1, o `expectedCount`) |
| `SQL-DELETE-INC` | 500 | `CodeDeleteInc` | `crud.go:204` | **delete incoerente**: righe cancellate diverse da 1 |
| `NOT-FOUND` | 404 | — | `crud.go:35,56` | `sql.ErrNoRows` in `GetById`/`GetByFilter`, allegato come causa |
| `NOT-FOUND` | 404 | — | `crud.go:200` | delete che non ha toccato nessuna riga |

## Cambiamenti rispetto al censimento precedente

- **27 siti su 32 ricadevano su `TECH500`**: un filtro malformato, un `SELECT` fallito e una
  transazione andata in rollback avevano lo stesso codice. La distinzione utile è soprattutto
  `SQL-FILTER` — è l'unico caso in cui il database non è mai stato interrogato, quindi non ha
  senso cercare il guasto lì.
- Tutti gli errori dichiarano ora la libreria di origine in `Ambit`.

## Note

- Ciò che arriva dal database (violazione di vincolo, deadlock, timeout) porta il codice
  dell'**operazione** e il messaggio del driver come causa: il codice dice *cosa stavamo
  facendo*, la causa *cosa è andato storto*.
- **Trappola nota** in `ExecTransaction`: non ritornare direttamente un `*ApplicationError` nil
  tipizzato come `error` — va convertito con un `if`, altrimenti la transazione risulta fallita.

## Errori sentinella

`locker/` non definisce codici propri: ritorna `lock.ErrNotAcquired` (riga di lock in
`scheduler_locks` non scaduta) e `lock.ErrLockLost` (rinnovo fallito) di `go-core-app/lock`.
