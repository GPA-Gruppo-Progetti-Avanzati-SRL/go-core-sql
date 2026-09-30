package coresql

import (
	"strings"
	"testing"

	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app/page"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

// TestApplySort_QuotaEValida: le colonne del sort sono identificatori quotati dal dialetto, e un
// campo che non è un identificatore è un errore prima che la query parta. La vecchia SortToSQL le
// interpolava con Sprintf: `?sort=id;DROP TABLE x` finiva nel SQL così com'era.
func TestApplySort_QuotaEValida(t *testing.T) {
	db := bun.NewDB(nil, sqlitedialect.New())
	q, err := ApplySort(db.NewSelect().TableExpr("people"), page.SortRequest{{Field: "name", Dir: page.Asc}, {Field: "created_at", Dir: page.Desc}})
	if err != nil {
		t.Fatal(err)
	}
	sql := q.String()
	if !strings.Contains(sql, `ORDER BY "name" ASC, "created_at" DESC`) {
		t.Fatalf("ORDER BY inatteso: %s", sql)
	}

	if _, err := ApplySort(db.NewSelect().TableExpr("people"), page.SortRequest{{Field: "id; DROP TABLE people", Dir: page.Asc}}); err == nil {
		t.Fatal("campo non identificatore: atteso errore")
	}
}
