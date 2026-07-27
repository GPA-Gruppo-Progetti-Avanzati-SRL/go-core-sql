package coresql

import (
	"database/sql"
	"testing"

	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/feature"
	"github.com/uptrace/bun/schema"
)

// fakeDialect is a minimal schema.Dialect used to exercise the feature-flag and
// Oracle-detection logic without importing any concrete dialect module (bun
// ships pgdialect/sqlitedialect/oracledialect as separate modules that are not
// dependencies of go-core-sql).
type fakeDialect struct {
	schema.BaseDialect
	name     dialect.Name
	features feature.Feature
}

func newFakeDialect(name dialect.Name, features feature.Feature) *fakeDialect {
	return &fakeDialect{name: name, features: features}
}

func (d *fakeDialect) Init(*sql.DB)                                               {}
func (d *fakeDialect) Name() dialect.Name                                         { return d.name }
func (d *fakeDialect) Features() feature.Feature                                  { return d.features }
func (d *fakeDialect) Tables() *schema.Tables                                     { return nil }
func (d *fakeDialect) OnTable(*schema.Table)                                      {}
func (d *fakeDialect) IdentQuote() byte                                           { return '"' }
func (d *fakeDialect) AppendSequence([]byte, *schema.Table, *schema.Field) []byte { return nil }
func (d *fakeDialect) DefaultVarcharLen() int                                     { return 0 }
func (d *fakeDialect) DefaultSchema() string                                      { return "" }

func TestWithOffsetFetch_AddsFeatureAndPromotes(t *testing.T) {
	base := newFakeDialect(dialect.Oracle, feature.Returning|feature.CTE)

	wrapped := WithOffsetFetch(base)
	if !wrapped.Features().Has(feature.OffsetFetch) {
		t.Fatal("WithOffsetFetch did not advertise feature.OffsetFetch")
	}
	// Original features must be preserved.
	if !wrapped.Features().Has(feature.Returning) || !wrapped.Features().Has(feature.CTE) {
		t.Fatal("WithOffsetFetch dropped one or more original features")
	}
	// Other interface methods must be promoted to the wrapped dialect.
	if wrapped.Name() != dialect.Oracle {
		t.Fatalf("Name() not promoted: got %v", wrapped.Name())
	}
	if wrapped.IdentQuote() != '"' {
		t.Fatalf("IdentQuote() not promoted: got %q", wrapped.IdentQuote())
	}
}

func TestApplyDialectWorkarounds_Oracle(t *testing.T) {
	oracle := newFakeDialect(dialect.Oracle, 0)
	patched := applyDialectWorkarounds(oracle)
	if !patched.Features().Has(feature.OffsetFetch) {
		t.Fatal("Oracle dialect should have OffsetFetch applied automatically")
	}
}

func TestApplyDialectWorkarounds_NonOracleUntouched(t *testing.T) {
	pg := newFakeDialect(dialect.PG, feature.Returning)
	patched := applyDialectWorkarounds(pg)
	if patched.Features().Has(feature.OffsetFetch) {
		t.Fatal("non-Oracle dialect must not get OffsetFetch")
	}
	if patched != schema.Dialect(pg) {
		t.Fatal("non-Oracle dialect must be returned unchanged (identity)")
	}
}
