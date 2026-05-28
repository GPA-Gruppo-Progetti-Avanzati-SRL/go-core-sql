package coresql

import "context"

// IRecord is implemented by any struct that maps to a SQL table.
// Fields must use `bun:"column:name"` tags for column mapping (or rely on
// bun's default snake_case conversion). Use `bun:",pk"` to mark primary keys.
type IRecord interface {
	GetTableName(ctx context.Context) string
}
