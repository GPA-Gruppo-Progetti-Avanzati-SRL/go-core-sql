package coresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app"

	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app/page"
	"github.com/rs/zerolog/log"
	"github.com/uptrace/bun"
)

// Le operazioni CRUD generiche sono metodi di *Service (metodi generici, Go 1.27+):
// il destinatario delle query è s.idb, quindi lo stesso metodo funziona sul
// *bun.DB o sulla *bun.Tx del Service passato a ExecTransaction.
//
// Nota sul linguaggio: un metodo generico non può implementare un metodo di
// interfaccia, quindi *Service non è assegnabile a un'interfaccia che dichiari
// questi CRUD. Il data layer dell'app espone i propri metodi concreti (IData) e
// richiama i metodi del Service al loro interno.

// GetById retrieves a record by its primary key column "id".
// For custom primary key names use GetByFilter.
func (s *Service) GetById[T IRecord](ctx context.Context, id any) (*T, *core.Error) {
	var result T
	table := result.GetTableName(ctx)
	err := s.idb.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where("id = ?", id).
		Scan(ctx, &result)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.NotFound().WithCause(err)
		}
		return nil, errs.Tech(CodeSelect).WithCause(err)
	}
	return &result, nil
}

// GetByFilter retrieves the first record matching the filter.
func (s *Service) GetByFilter[T IRecord](ctx context.Context, filter IFilter) (*T, *core.Error) {
	var result T
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return nil, errs.Tech(CodeFilter).WithCause(err)
	}
	if err := s.idb.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Limit(1).
		Scan(ctx, &result); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errs.NotFound().WithCause(err)
		}
		return nil, errs.Tech(CodeSelect).WithCause(err)
	}
	return &result, nil
}

// GetAllByFilter retrieves all records matching the filter.
func (s *Service) GetAllByFilter[T IRecord](ctx context.Context, filter IFilter) ([]*T, *core.Error) {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return nil, errs.Tech(CodeFilter).WithCause(err)
	}
	var results []*T
	if err := s.idb.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Scan(ctx, &results); err != nil {
		return nil, errs.Tech(CodeSelect).WithCause(err)
	}
	return results, nil
}

// GetAllByFilterSorted retrieves all records matching the filter, ordered by sort.
func (s *Service) GetAllByFilterSorted[T IRecord](ctx context.Context, filter IFilter, sort page.SortRequest) ([]*T, *core.Error) {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return nil, errs.Tech(CodeFilter).WithCause(err)
	}
	q := s.idb.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...)
	q, err = ApplySort(q, sort)
	if err != nil {
		return nil, errs.Business(CodeSort).WithCause(err)
	}
	var results []*T
	if err := q.Scan(ctx, &results); err != nil {
		return nil, errs.Tech(CodeSelect).WithCause(err)
	}
	return results, nil
}

// InsertOne inserts a single record. obj must be a pointer to a struct with bun tags.
// The table name is derived from the struct name (snake_case) or the bun:"table:..." tag.
// To use a custom table name, embed bun.BaseModel with the bun:"table:..." tag.
func (s *Service) InsertOne(ctx context.Context, obj IRecord) *core.Error {
	if _, err := s.idb.NewInsert().
		Model(obj).
		Exec(ctx); err != nil {
		return errs.Tech(CodeInsert).WithCause(err)
	}
	return nil
}

// InsertMany bulk-inserts records of type T in a single statement.
// The table name is derived from the struct name (snake_case) or the bun:"table:..." tag.
// To use a custom table name, embed bun.BaseModel with the bun:"table:..." tag.
func (s *Service) InsertMany[T IRecord](ctx context.Context, objs []*T) *core.Error {
	if len(objs) == 0 {
		return nil
	}
	if _, err := s.idb.NewInsert().
		Model(&objs).
		Exec(ctx); err != nil {
		return errs.Tech(CodeInsert).WithCause(err)
	}
	return nil
}

// UpdateOne updates a single record matching filter with the given column→value map.
// Returns an error if the number of affected rows is not exactly 1.
func (s *Service) UpdateOne(ctx context.Context, filter IFilter, set map[string]any) *core.Error {
	table := filter.GetFilterTableName(ctx)
	where, whereArgs, appErr := buildWriteWhere(filter)
	if appErr != nil {
		return appErr
	}
	if len(set) == 0 {
		return errs.Tech(CodeEmptySet).WithMessage("no fields to update")
	}
	q := s.idb.NewUpdate().TableExpr("?", bun.Ident(table))
	for col, val := range set {
		q = q.Set("? = ?", bun.Ident(col), val)
	}
	res, err := q.Where(where, whereArgs...).Exec(ctx)
	if err != nil {
		log.Error().Err(err).Msgf("UpdateOne failed on %s", table)
		return errs.Tech(CodeUpdate).WithCause(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		log.Error().Msgf("UpdateOne on %s: expected 1 row, got %d", table, n)
		return errs.Tech(CodeUpdateInc).WithMessage(fmt.Sprintf("expected 1 row updated, got %d", n))
	}
	return nil
}

// UpdateMany updates all records matching filter. Returns an error if affected rows ≠ expectedCount.
func (s *Service) UpdateMany(ctx context.Context, filter IFilter, set map[string]any, expectedCount int) *core.Error {
	table := filter.GetFilterTableName(ctx)
	where, whereArgs, appErr := buildWriteWhere(filter)
	if appErr != nil {
		return appErr
	}
	if len(set) == 0 {
		return errs.Tech(CodeEmptySet).WithMessage("no fields to update")
	}
	q := s.idb.NewUpdate().TableExpr("?", bun.Ident(table))
	for col, val := range set {
		q = q.Set("? = ?", bun.Ident(col), val)
	}
	res, err := q.Where(where, whereArgs...).Exec(ctx)
	if err != nil {
		log.Error().Err(err).Msgf("UpdateMany failed on %s", table)
		return errs.Tech(CodeUpdate).WithCause(err)
	}
	n, _ := res.RowsAffected()
	if int(n) != expectedCount {
		log.Error().Msgf("UpdateMany on %s: expected %d rows, got %d", table, expectedCount, n)
		return errs.Tech(CodeUpdateInc).WithMessage(fmt.Sprintf("expected %d rows updated, got %d", expectedCount, n))
	}
	return nil
}

// DeleteOne deletes the single record matching the filter.
// Returns NotFoundError if no row was deleted.
func (s *Service) DeleteOne(ctx context.Context, filter IFilter) *core.Error {
	table := filter.GetFilterTableName(ctx)
	where, args, appErr := buildWriteWhere(filter)
	if appErr != nil {
		return appErr
	}
	res, err := s.idb.NewDelete().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Exec(ctx)
	if err != nil {
		log.Error().Err(err).Msgf("DeleteOne failed on %s", table)
		return errs.Tech(CodeDelete).WithCause(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errs.NotFound()
	}
	if n != 1 {
		log.Error().Msgf("DeleteOne on %s: affected %d rows", table, n)
		return errs.Tech(CodeDeleteInc).WithMessage(fmt.Sprintf("expected 1 row deleted, got %d", n))
	}
	return nil
}

// DeleteMany deletes all records matching the filter.
func (s *Service) DeleteMany(ctx context.Context, filter IFilter) *core.Error {
	table := filter.GetFilterTableName(ctx)
	where, args, appErr := buildWriteWhere(filter)
	if appErr != nil {
		return appErr
	}
	if _, err := s.idb.NewDelete().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Exec(ctx); err != nil {
		log.Error().Err(err).Msgf("DeleteMany failed on %s", table)
		return errs.Tech(CodeDelete).WithCause(err)
	}
	return nil
}

// CountRows counts records matching the filter.
func (s *Service) CountRows(ctx context.Context, filter IFilter) (int64, *core.Error) {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return 0, errs.Tech(CodeFilter).WithCause(err)
	}
	count, err := s.idb.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...).
		Count(ctx)
	if err != nil {
		return 0, errs.Tech(CodeCount).WithCause(err)
	}
	return int64(count), nil
}

// GetPageByFilter returns a paginated set of records matching the filter.
// paging.SetTotalItems is called so callers can inspect total count.
func (s *Service) GetPageByFilter[T IRecord](ctx context.Context, filter IFilter, paging *page.Paging) ([]T, *core.Error) {
	table := filter.GetFilterTableName(ctx)
	where, args, err := buildWhere(filter)
	if err != nil {
		return nil, errs.Tech(CodeFilter).WithCause(err)
	}

	base := s.idb.NewSelect().
		TableExpr("?", bun.Ident(table)).
		Where(where, args...)

	total, err := base.Count(ctx)
	if err != nil {
		return nil, errs.Tech(CodeCount).WithCause(err)
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
		return nil, errs.Tech(CodeSelect).WithCause(err)
	}
	return results, nil
}

// NextSequenceValue returns the next value from the named PostgreSQL sequence.
func (s *Service) NextSequenceValue(ctx context.Context, seqName string) (int64, *core.Error) {
	var id int64
	if err := s.idb.NewRaw("SELECT nextval(?::regclass)", seqName).Scan(ctx, &id); err != nil {
		return 0, errs.Tech(CodeSequence).WithCause(err)
	}
	return id, nil
}

// buildWriteWhere è buildWhere per le scritture (UpdateOne/UpdateMany/DeleteOne/DeleteMany): un
// filtro che non produce nessuna condizione è un errore, non una WHERE 1=1. Un filtro coi campi
// tutti `omitempty` e tutti vuoti — tipicamente query param assenti — faceva aggiornare o cancellare
// l'intera tabella (DeleteOne: una riga qualsiasi). Chi vuole davvero toccare tutte le righe lo
// scrive con una query bun esplicita.
func buildWriteWhere(filter IFilter) (string, []any, *core.Error) {
	where, args, err := buildWhere(filter)
	if err != nil {
		return "", nil, errs.Tech(CodeFilter).WithCause(err)
	}
	if where == matchAll {
		return "", nil, errs.Business(CodeEmptyFilter).
			WithMessage("il filtro non esprime nessuna condizione: la scrittura toccherebbe tutte le righe")
	}
	return where, args, nil
}
