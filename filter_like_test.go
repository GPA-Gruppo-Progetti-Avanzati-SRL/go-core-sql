package coresql

import (
	"context"
	"database/sql"
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
