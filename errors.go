package coresql

import (
	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app"
)

// Ambit è la libreria di origine dell'errore. I costruttori di core mettono in Ambit l'AppName,
// cioè l'applicazione che l'errore lo *riceve*: senza sovrascriverlo un guasto del driver SQL si
// presenta come un errore dell'app, e chi legge il log non sa in quale libreria guardare.
const Ambit = "go-core-sql"

// Codici degli errori emessi dal modulo. SQL-*-INC e SQL-EMPTY-SET sono le condizioni
// diagnosticate dalla libreria; gli altri marcano l'operazione bun/database-sql fallita, con
// l'errore del driver allegato come causa.
const (
	CodeFilter      = "SQL-FILTER"     // buildWhere fallita: tag col/op non validi
	CodeSelect      = "SQL-SELECT"     // SELECT/Scan fallita
	CodeCount       = "SQL-COUNT"      // Count fallita
	CodeInsert      = "SQL-INSERT"     // INSERT fallita
	CodeUpdate      = "SQL-UPDATE"     // UPDATE fallita
	CodeDelete      = "SQL-DELETE"     // DELETE fallita
	CodeTransaction = "SQL-TX"         // RunInTx fallita (o rollback dal callback)
	CodeSequence    = "SQL-SEQ"        // nextval fallita
	CodeEmptySet    = "SQL-EMPTY-SET"  // update senza campi: la clausola SET sarebbe vuota
	CodeUpdateInc   = "SQL-UPDATE-INC" // righe aggiornate diverse dalle attese
	CodeDeleteInc   = "SQL-DELETE-INC" // righe cancellate diverse dalle attese
)

// techErr è il costruttore usato da tutto il modulo: un errore tecnico che dichiara sempre
// codice e libreria di origine.
func techErr(code string) *core.ApplicationError {
	return core.TechnicalError().WithAmbit(Ambit).WithCode(code)
}

// notFound è il 404 del modulo (codice NOT-FOUND di core), con la libreria di origine.
func notFound() *core.ApplicationError {
	return core.NotFoundError().WithAmbit(Ambit)
}
