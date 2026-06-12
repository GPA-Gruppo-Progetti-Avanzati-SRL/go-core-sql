package coresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	core "github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app"
	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app/page"
	"github.com/rs/zerolog/log"
	"github.com/uptrace/bun"
)

// GetById retrieves a record by its primary key column "id".
// For custom primary key names use GetByFilter.
func GetById[T IRecord](ctx context.Context, db bun.IDB, id any) (*T, *core.ApplicationError) {
	var result T
	table := result.GetTableName(ctx)
	err := db.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where("id = ?", id).
		Scan(ctx, &result)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, core.NotFoundError()
		}
		return nil, core.TechnicalErrorWithError(err)
	}
	return &result, nil
}

// GetByFilter retrieves the first record matching the filter.
func GetByFilter[T IRecord](ctx context.Context, db bun.IDB, filter IFilter) (*T, *core.ApplicationError) {
	var result T
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return nil, core.TechnicalErrorWithError(err)
	}
	if err := db.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Limit(1).
		Scan(ctx, &result); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, core.NotFoundError()
		}
		return nil, core.TechnicalErrorWithError(err)
	}
	return &result, nil
}

// GetAllByFilter retrieves all records matching the filter.
func GetAllByFilter[T IRecord](ctx context.Context, db bun.IDB, filter IFilter) ([]*T, *core.ApplicationError) {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return nil, core.TechnicalErrorWithError(err)
	}
	var results []*T
	if err := db.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Scan(ctx, &results); err != nil {
		return nil, core.TechnicalErrorWithError(err)
	}
	return results, nil
}

// GetAllByFilterSorted retrieves all records matching the filter, ordered by sort.
func GetAllByFilterSorted[T IRecord](ctx context.Context, db bun.IDB, filter IFilter, sort page.SortRequest) ([]*T, *core.ApplicationError) {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return nil, core.TechnicalErrorWithError(err)
	}
	q := db.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...)
	if expr := sortExpr(sort); expr != "" {
		q = q.OrderExpr(expr)
	}
	var results []*T
	if err := q.Scan(ctx, &results); err != nil {
		return nil, core.TechnicalErrorWithError(err)
	}
	return results, nil
}

// InsertOne inserts a single record. obj must be a pointer to a struct with bun tags.
// The table name is derived from the struct name (snake_case) or the bun:"table:..." tag.
// To use a custom table name, embed bun.BaseModel with the bun:"table:..." tag.
func InsertOne(ctx context.Context, db bun.IDB, obj IRecord) *core.ApplicationError {
	if _, err := db.NewInsert().
		Model(obj).
		Exec(ctx); err != nil {
		return core.TechnicalErrorWithError(err)
	}
	return nil
}

// InsertMany bulk-inserts records of type T in a single statement.
// The table name is derived from the struct name (snake_case) or the bun:"table:..." tag.
// To use a custom table name, embed bun.BaseModel with the bun:"table:..." tag.
func InsertMany[T IRecord](ctx context.Context, db bun.IDB, objs []*T) *core.ApplicationError {
	if len(objs) == 0 {
		return nil
	}
	if _, err := db.NewInsert().
		Model(&objs).
		Exec(ctx); err != nil {
		return core.TechnicalErrorWithError(err)
	}
	return nil
}

// UpdateOne updates a single record matching filter with the given column→value map.
// Returns an error if the number of affected rows is not exactly 1.
func UpdateOne(ctx context.Context, db bun.IDB, filter IFilter, set map[string]any) *core.ApplicationError {
	table := filter.GetFilterTableName(ctx)
	where, whereArgs, err := buildWhere(filter)
	if err != nil {
		return core.TechnicalErrorWithError(err)
	}
	if len(set) == 0 {
		return core.TechnicalErrorWithCodeAndMessage("SQL-EMPTY-SET", "no fields to update")
	}
	q := db.NewUpdate().TableExpr("?", bun.Ident(table))
	for col, val := range set {
		q = q.Set("? = ?", bun.Ident(col), val)
	}
	res, err := q.Where(where, whereArgs...).Exec(ctx)
	if err != nil {
		log.Error().Err(err).Msgf("UpdateOne failed on %s", table)
		return core.TechnicalErrorWithError(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		log.Error().Msgf("UpdateOne on %s: expected 1 row, got %d", table, n)
		return core.TechnicalErrorWithCodeAndMessage("SQL-UPDATE-INC", fmt.Sprintf("expected 1 row updated, got %d", n))
	}
	return nil
}

// UpdateMany updates all records matching filter. Returns an error if affected rows ≠ expectedCount.
func UpdateMany(ctx context.Context, db bun.IDB, filter IFilter, set map[string]any, expectedCount int) *core.ApplicationError {
	table := filter.GetFilterTableName(ctx)
	where, whereArgs, err := buildWhere(filter)
	if err != nil {
		return core.TechnicalErrorWithError(err)
	}
	if len(set) == 0 {
		return core.TechnicalErrorWithCodeAndMessage("SQL-EMPTY-SET", "no fields to update")
	}
	q := db.NewUpdate().TableExpr("?", bun.Ident(table))
	for col, val := range set {
		q = q.Set("? = ?", bun.Ident(col), val)
	}
	res, err := q.Where(where, whereArgs...).Exec(ctx)
	if err != nil {
		log.Error().Err(err).Msgf("UpdateMany failed on %s", table)
		return core.TechnicalErrorWithError(err)
	}
	n, _ := res.RowsAffected()
	if int(n) != expectedCount {
		log.Error().Msgf("UpdateMany on %s: expected %d rows, got %d", table, expectedCount, n)
		return core.TechnicalErrorWithCodeAndMessage("SQL-UPDATE-INC", fmt.Sprintf("expected %d rows updated, got %d", expectedCount, n))
	}
	return nil
}

// DeleteOne deletes the single record matching the filter.
// Returns NotFoundError if no row was deleted.
func DeleteOne(ctx context.Context, db bun.IDB, filter IFilter) *core.ApplicationError {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return core.TechnicalErrorWithError(err)
	}
	res, err := db.NewDelete().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Exec(ctx)
	if err != nil {
		log.Error().Err(err).Msgf("DeleteOne failed on %s", table)
		return core.TechnicalErrorWithError(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return core.NotFoundError()
	}
	if n != 1 {
		log.Error().Msgf("DeleteOne on %s: affected %d rows", table, n)
		return core.TechnicalErrorWithCodeAndMessage("SQL-DELETE-INC", fmt.Sprintf("expected 1 row deleted, got %d", n))
	}
	return nil
}

// DeleteMany deletes all records matching the filter.
func DeleteMany(ctx context.Context, db bun.IDB, filter IFilter) *core.ApplicationError {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return core.TechnicalErrorWithError(err)
	}
	if _, err := db.NewDelete().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Exec(ctx); err != nil {
		log.Error().Err(err).Msgf("DeleteMany failed on %s", table)
		return core.TechnicalErrorWithError(err)
	}
	return nil
}

// CountRows counts records matching the filter.
func CountRows(ctx context.Context, db bun.IDB, filter IFilter) (int64, *core.ApplicationError) {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return 0, core.TechnicalErrorWithError(err)
	}
	count, err := db.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Count(ctx)
	if err != nil {
		return 0, core.TechnicalErrorWithError(err)
	}
	return int64(count), nil
}

// GetPageByFilter returns a paginated set of records matching the filter.
// paging.SetTotalItems is called so callers can inspect total count.
func GetPageByFilter[T IRecord](ctx context.Context, db bun.IDB, filter IFilter, paging *page.Paging) ([]T, *core.ApplicationError) {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return nil, core.TechnicalErrorWithError(err)
	}

	base := db.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...)

	total, err := base.Count(ctx)
	if err != nil {
		return nil, core.TechnicalErrorWithError(err)
	}
	paging.SetTotalItems(int64(total))

	offset, appErr := paging.Paging()
	if appErr != nil {
		return nil, appErr
	}

	if offset >= 0 {
		base = base.Offset(offset).Limit(paging.PageSize)
	}

	var results []T
	if err := base.Scan(ctx, &results); err != nil {
		return nil, core.TechnicalErrorWithError(err)
	}
	return results, nil
}

// ExecTransaction runs fn inside a database transaction.
// The transaction is automatically rolled back on error and committed on success.
// fn receives a bun.IDB so it can call any CRUD helper from this package.
func ExecTransaction(ctx context.Context, db *bun.DB, fn func(ctx context.Context, tx bun.IDB) error) *core.ApplicationError {
	if err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(ctx, &tx)
	}); err != nil {
		return core.TechnicalErrorWithError(err)
	}
	return nil
}

// NextSequenceValue returns the next value from the named PostgreSQL sequence.
func NextSequenceValue(ctx context.Context, db bun.IDB, seqName string) (int64, *core.ApplicationError) {
	var id int64
	if err := db.NewRaw("SELECT nextval(?::regclass)", seqName).Scan(ctx, &id); err != nil {
		return 0, core.TechnicalErrorWithError(err)
	}
	return id, nil
}
