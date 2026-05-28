package coresql

import (
	"fmt"
	"strings"

	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app/page"
)

// SortToSQL converts a SortRequest to a full "ORDER BY col ASC, col2 DESC" clause.
// Returns an empty string when sort is empty.
func SortToSQL(s page.SortRequest) string {
	expr := sortExpr(s)
	if expr == "" {
		return ""
	}
	return "ORDER BY " + expr
}

// sortExpr returns the expression part of ORDER BY without the keyword,
// for use with bun's OrderExpr method.
func sortExpr(s page.SortRequest) string {
	if len(s) == 0 {
		return ""
	}
	parts := make([]string, 0, len(s))
	for _, f := range s {
		dir := "ASC"
		if f.Dir == page.Desc {
			dir = "DESC"
		}
		parts = append(parts, fmt.Sprintf("%s %s", f.Field, dir))
	}
	return strings.Join(parts, ", ")
}
