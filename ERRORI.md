# Codici di errore — go-core-sql

I metodi generici di `*coresql.Service` ritornano `*core.ApplicationError`. L'errore di bun /
`database/sql` è sempre allegato come causa: un `sql.ErrNoRows` resta raggiungibile con
`errors.Is` anche dentro il 404.

## Codici emessi

| Codice | HTTP | Origine | Significato |
|---|---|---|---|
| `NOT-FOUND` | 404 | `crud.go:35` (`GetById`), `crud.go:56` (`GetByFilter`) | nessuna riga: causa `sql.ErrNoRows` allegata |
| `NOT-FOUND` | 404 | `crud.go:200` (`DeleteOne`) | delete che non ha toccato nessuna riga |
| `SQL-EMPTY-SET` | 500 | `crud.go:136` (`UpdateOne`), `crud.go:163` (`UpdateMany`) | nessun campo da aggiornare: la clausola `SET` sarebbe vuota. È un errore di costruzione della query, non del DB |
| `SQL-UPDATE-INC` | 500 | `crud.go:150` | **update incoerente**: attesa 1 riga aggiornata, ottenute `n` |
| `SQL-UPDATE-INC` | 500 | `crud.go:177` | update multiplo incoerente: attese `expectedCount` righe, ottenute `n` |
| `SQL-DELETE-INC` | 500 | `crud.go:204` | **delete incoerente**: attesa 1 riga cancellata, ottenute `n` |
| `TECH500` | 500 | ovunque nei CRUD (`crud.go:37,48,58,68,75,85,95,107,122,133,145,160,172,188,196,214,221,231,238,249,258,273,282`) e in `service.go:44` (`ExecTransaction`) | errore del driver/bun senza codice specifico: default di `TechnicalError()`, con l'errore SQL in causa |

## Note

- I codici `SQL-*` marcano le sole condizioni **diagnosticate dalla libreria** (set vuoto,
  conteggio righe inatteso). Tutto ciò che arriva dal database resta `TECH500` con la causa:
  il messaggio del driver (violazione di vincolo, deadlock, timeout) è il testo esposto.
- **Trappola nota** in `ExecTransaction`: non ritornare direttamente un `*ApplicationError` nil
  tipizzato come `error` — va convertito con un `if`, altrimenti la transazione viene vista
  come fallita.

## Errori sentinella

`locker/` non definisce codici propri: ritorna `lock.ErrNotAcquired` (lease già tenuto o riga
di lock non scaduta nella `scheduler_locks`) e `lock.ErrLockLost` (rinnovo fallito) di
`go-core-app/lock`.
