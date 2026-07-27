package coresql

import (
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/feature"
	"github.com/uptrace/bun/schema"
)

// featureDialect wraps a schema.Dialect, advertising extra feature flags on top
// of those the wrapped dialect already reports. Every other method of the
// schema.Dialect interface (Init, Tables, Append*, AppendSequence, …) is
// promoted to the embedded dialect unchanged, so the wrapper behaves exactly
// like the original except for Features().
//
// This is safe because bun reads the feature set purely through the interface
// (schema.QueryGen.Dialect().Features().Has(...)) and never type-asserts the
// dialect to its concrete type in the query-building path.
type featureDialect struct {
	schema.Dialect
	extra feature.Feature
}

func (d featureDialect) Features() feature.Feature {
	return d.Dialect.Features() | d.extra
}

// WithOffsetFetch wraps d so it advertises feature.OffsetFetch. With that flag
// set, bun emits the SQL:2008 pagination syntax
//
//	OFFSET n ROWS FETCH NEXT m ROWS ONLY
//
// instead of the generic
//
//	LIMIT m OFFSET n
//
// The generic form is rejected by Oracle (ORA-00933), whose bundled bun dialect
// omits feature.OffsetFetch even though Oracle supports OFFSET/FETCH since 12c.
// Wrap the dialect once at NewService call-site to fix pagination
// (GetPageByFilter) and any manual .Limit()/.Offset() across the whole layer:
//
//	db, err := coresql.NewService(cfg, coresql.WithOffsetFetch(oracledialect.New()), lc)
//
// For non-Oracle dialects that already report the flag (e.g. MSSQL) the wrap is
// a harmless no-op.
func WithOffsetFetch(d schema.Dialect) schema.Dialect {
	return featureDialect{Dialect: d, extra: feature.OffsetFetch}
}

// applyDialectWorkarounds patches a dialect for known bun shortcomings before it
// is handed to bun.NewDB. It is called by NewService, so applications get the
// correct SQL without having to wrap the dialect themselves.
//
// Currently: the Oracle dialect does not advertise feature.OffsetFetch, so bun
// would emit the generic "LIMIT n OFFSET m" that Oracle rejects (ORA-00933).
// This breaks GetPageByFilter and GetByFilter (which uses .Limit(1)). The wrap
// is gated on dialect.Name() so we detect Oracle without importing the
// oracledialect package (keeping go-core-sql dialect-agnostic).
func applyDialectWorkarounds(d schema.Dialect) schema.Dialect {
	if d.Name() == dialect.Oracle {
		return WithOffsetFetch(d)
	}
	return d
}
