package coresql

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"
)

type likeFilter struct {
	Contains string `col:"name" op:"CONTAINS" omitempty:"true"`
	Starts   string `col:"name" op:"STARTSWITH" omitempty:"true"`
	Ends     string `col:"name" op:"ENDSWITH" omitempty:"true"`
}

func (likeFilter) GetFilterTableName(context.Context) string { return "items" }

// TestBuildWhere_LikeLetterale: STARTSWITH/ENDSWITH/CONTAINS confrontano un testo. Un `%` o un `_`
// nel valore — arrivato da un query param — deve restare un carattere, non diventare un jolly:
// prima `CONTAINS "%"` selezionava tutte le righe. Provato su SQLite vero, perché la sintassi di
// ESCAPE è ciò che conta.
func TestBuildWhere_LikeLetterale(t *testing.T) {
	sqldb, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	db := bun.NewDB(sqldb, sqlitedialect.New())
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, `CREATE TABLE items (name TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"sconto 50%", "sconto 50 euro", "a_b", "axb", "!bang"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO items (name) VALUES (?)`, n); err != nil {
			t.Fatal(err)
		}
	}
	count := func(f likeFilter) []string {
		t.Helper()
		where, args, err := buildWhere(f)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if err := db.NewSelect().TableExpr("items").Column("name").Where(where, args...).Order("name").Scan(ctx, &names); err != nil {
			t.Fatalf("%s %v: %v", where, args, err)
		}
		return names
	}

	cases := []struct {
		f    likeFilter
		want []string
	}{
		{likeFilter{Contains: "%"}, []string{"sconto 50%"}},
		{likeFilter{Contains: "_"}, []string{"a_b"}},
		{likeFilter{Starts: "a_"}, []string{"a_b"}},
		{likeFilter{Ends: "50%"}, []string{"sconto 50%"}},
		{likeFilter{Starts: "!"}, []string{"!bang"}},
		{likeFilter{Contains: "sconto"}, []string{"sconto 50 euro", "sconto 50%"}},
	}
	for _, c := range cases {
		got := count(c.f)
		if len(got) != len(c.want) {
			t.Errorf("%+v: righe %v, attese %v", c.f, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%+v: righe %v, attese %v", c.f, got, c.want)
				break
			}
		}
	}
}

type opFilter struct {
	Eq     string   `col:"name"   op:"="       omitempty:"true"`
	Ne     string   `col:"name"   op:"!="      omitempty:"true"`
	Gt     int      `col:"n"      op:">"       omitempty:"true"`
	In     []string `col:"name"   op:"IN"      omitempty:"true"`
	NotIn  []string `col:"name"   op:"NOT IN"  omitempty:"true"`
	IsNull bool     `col:"note"   op:"IS NULL" omitempty:"true"`
}

func (opFilter) GetFilterTableName(context.Context) string { return "ops" }

// Gli operatori del filter builder, eseguiti su SQLite: è la sintassi che arriva al database a dire
// se un operatore funziona, non la stringa che il builder produce.
func TestBuildWhere_Operatori(t *testing.T) {
	sqldb, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqldb.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	db := bun.NewDB(sqldb, sqlitedialect.New())
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, `CREATE TABLE ops (name TEXT, n INTEGER, note TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		name string
		n    int
		note any
	}{{"a", 1, nil}, {"b", 2, "x"}, {"c", 3, nil}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO ops VALUES (?, ?, ?)`, r.name, r.n, r.note); err != nil {
			t.Fatal(err)
		}
	}
	names := func(f opFilter) string {
		t.Helper()
		where, args, err := buildWhere(f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		if err := db.NewSelect().TableExpr("ops").Column("name").Where(where, args...).Order("name").Scan(ctx, &out); err != nil {
			t.Fatalf("%s %v: %v", where, args, err)
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct {
		nome string
		f    opFilter
		want string
	}{
		{"nessun campo = tutte", opFilter{}, "a,b,c"},
		{"=", opFilter{Eq: "b"}, "b"},
		{"!=", opFilter{Ne: "b"}, "a,c"},
		{">", opFilter{Gt: 1}, "b,c"},
		{"IN", opFilter{In: []string{"a", "c"}}, "a,c"},
		{"NOT IN", opFilter{NotIn: []string{"a"}}, "b,c"},
		{"IS NULL", opFilter{IsNull: true}, "a,c"},
		{"AND", opFilter{Gt: 1, IsNull: true}, "c"},
	} {
		if got := names(tc.f); got != tc.want {
			t.Errorf("%s: %q, atteso %q", tc.nome, got, tc.want)
		}
	}
	// IN su una slice vuota non seleziona nulla (IN () non è SQL valido), NOT IN vuoto tutto.
	if where, _, _ := buildCondition("name", "IN", []string{}); where != "1=0" {
		t.Errorf("IN vuoto = %q", where)
	}
	if _, _, err := buildCondition("name", "IN", "non-una-slice"); err == nil {
		t.Error("IN su un valore non slice: atteso errore")
	}
	if _, _, err := buildCondition("name", "BOH", 1); err == nil {
		t.Error("operatore sconosciuto: atteso errore")
	}
}
