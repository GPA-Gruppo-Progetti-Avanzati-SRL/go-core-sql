package coresql

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// IFilter is implemented by filter structs used to build SQL WHERE clauses.
//
// Tag each field with:
//
//	col:"column_name"   — SQL column name
//	op:"operator"       — operator (see buildCondition for the full list)
//	omitempty:"true"    — skip the field when its value is the zero value
//
// Example:
//
//	type userFilter struct {
//	    Status string   `col:"status" op:"=" omitempty:"true"`
//	    IDs    []string `col:"id"     op:"IN" omitempty:"true"`
//	}
//	func (f userFilter) GetFilterTableName(ctx context.Context) string { return "users" }
type IFilter interface {
	GetFilterTableName(ctx context.Context) string
}

// buildWhere converts a tagged filter struct into a WHERE clause string and
// the corresponding positional arguments. The clause uses ? placeholders,
// which bun translates to the driver-specific syntax ($1, ?, etc.).
//
// Returns "1=1" with no args when no field produces a condition, so the
// clause is always safe to pass directly to bun's Where() method.
func buildWhere(f IFilter) (string, []any, error) {
	if f == nil {
		return "", nil, fmt.Errorf("filter cannot be nil")
	}
	val := reflect.ValueOf(f)
	typ := reflect.TypeOf(f)
	if typ.Kind() == reflect.Pointer {
		if val.IsNil() {
			return "", nil, fmt.Errorf("filter cannot be a nil pointer")
		}
		val = val.Elem()
		typ = val.Type()
	}
	if typ.Kind() != reflect.Struct {
		return "", nil, fmt.Errorf("filter must be a struct")
	}

	var conditions []string
	var args []any

	for i := range typ.NumField() {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		col := field.Tag.Get("col")
		op := field.Tag.Get("op")
		if col == "" || op == "" {
			continue
		}
		if _, hasOmit := field.Tag.Lookup("omitempty"); hasOmit && fieldVal.IsZero() {
			continue
		}

		cond, condArgs, err := buildCondition(col, op, fieldVal.Interface())
		if err != nil {
			return "", nil, fmt.Errorf("field %q op %q: %w", col, op, err)
		}
		conditions = append(conditions, cond)
		args = append(args, condArgs...)
	}

	if len(conditions) == 0 {
		return "1=1", nil, nil
	}
	return strings.Join(conditions, " AND "), args, nil
}

// Supported operators: =, !=, >, >=, <, <=, IN, NOT IN, LIKE, ILIKE,
// STARTSWITH, ENDSWITH, CONTAINS, IS NULL, IS NOT NULL.
func buildCondition(col, op string, val any) (string, []any, error) {
	switch strings.ToUpper(op) {
	case "=", "!=", ">", ">=", "<", "<=":
		return fmt.Sprintf("%s %s ?", col, op), []any{val}, nil

	case "IN", "NOT IN":
		rv := reflect.ValueOf(val)
		if rv.Kind() != reflect.Slice {
			return "", nil, fmt.Errorf("operator %q requires a slice", op)
		}
		if rv.Len() == 0 {
			if strings.EqualFold(op, "IN") {
				return "1=0", nil, nil // IN () is always false
			}
			return "1=1", nil, nil // NOT IN () is always true
		}
		ph := make([]string, rv.Len())
		a := make([]any, rv.Len())
		for i := range rv.Len() {
			ph[i] = "?"
			a[i] = rv.Index(i).Interface()
		}
		return fmt.Sprintf("%s %s (%s)", col, strings.ToUpper(op), strings.Join(ph, ", ")), a, nil

	case "LIKE", "ILIKE":
		s, ok := val.(string)
		if !ok {
			return "", nil, fmt.Errorf("operator %q requires a string", op)
		}
		return fmt.Sprintf("%s %s ?", col, strings.ToUpper(op)), []any{s}, nil

	case "STARTSWITH":
		s, ok := val.(string)
		if !ok {
			return "", nil, fmt.Errorf("operator STARTSWITH requires a string")
		}
		return fmt.Sprintf("%s LIKE ?", col), []any{s + "%"}, nil

	case "ENDSWITH":
		s, ok := val.(string)
		if !ok {
			return "", nil, fmt.Errorf("operator ENDSWITH requires a string")
		}
		return fmt.Sprintf("%s LIKE ?", col), []any{"%" + s}, nil

	case "CONTAINS":
		s, ok := val.(string)
		if !ok {
			return "", nil, fmt.Errorf("operator CONTAINS requires a string")
		}
		return fmt.Sprintf("%s LIKE ?", col), []any{"%" + s + "%"}, nil

	case "IS NULL":
		return fmt.Sprintf("%s IS NULL", col), nil, nil

	case "IS NOT NULL":
		return fmt.Sprintf("%s IS NOT NULL", col), nil, nil

	default:
		return "", nil, fmt.Errorf("unsupported operator %q", op)
	}
}
