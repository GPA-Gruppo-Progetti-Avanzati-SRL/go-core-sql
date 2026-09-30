package coresql

import (
	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app/page"
	"github.com/uptrace/bun"
)

// ApplySort aggiunge a q un ORDER BY per ogni campo di s, nell'ordine dato.
//
// Ogni colonna passa da bun.Ident, quindi è quotata come identificatore dal dialetto e non
// interpolata nel SQL, e prima ancora s è validata (page.SortRequest.Validate): il sort arriva di
// solito da un query param, e la vecchia SortToSQL costruiva la clausola con Sprintf — con
// `?sort=id;DROP TABLE x` il testo finiva così com'era nella query. Un campo non valido è un
// errore, non un campo ignorato: un ordinamento diverso da quello chiesto è un risultato sbagliato
// che nessuno vede.
func ApplySort(q *bun.SelectQuery, s page.SortRequest) (*bun.SelectQuery, error) {
	if err := s.Validate(); err != nil {
		return q, err
	}
	for _, f := range s {
		dir := " ASC"
		if f.Dir == page.Desc {
			dir = " DESC"
		}
		q = q.OrderExpr("?"+dir, bun.Ident(f.Field))
	}
	return q, nil
}
