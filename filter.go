package coresql

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app/utils"
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
	// Lo scheletro (nil, puntatore, struct, tag, omitempty) è utils.TaggedFields, condiviso col
	// filter builder di go-core-mongo.
	fields, err := utils.TaggedFields(f, "col", "op")
	if err != nil {
		return "", nil, err
	}

	var conditions []string
	var args []any

	for _, tf := range fields {
		col, op := tf.Key, tf.Op
		cond, condArgs, err := buildCondition(col, op, tf.Value)
		if err != nil {
			return "", nil, fmt.Errorf("field %q op %q: %w", col, op, err)
		}
		conditions = append(conditions, cond)
		args = append(args, condArgs...)
	}

	if len(conditions) == 0 {
		return matchAll, nil, nil
	}
	return strings.Join(conditions, " AND "), args, nil
}

// matchAll è la WHERE di un filtro senza condizioni.
const matchAll = "1=1"

// likeEscape è il carattere di ESCAPE dei LIKE costruiti da STARTSWITH/ENDSWITH/CONTAINS. Non il
// backslash: in MySQL, dentro un letterale stringa, andrebbe raddoppiato, e la clausola non sarebbe
// più la stessa su tutti i dialetti. `!` non ha significato né in SQL né nei pattern LIKE.
const likeEscape = "!"

// escapeLike rende letterali i caratteri speciali del pattern LIKE (`%`, `_` e il carattere di
// escape stesso). STARTSWITH/ENDSWITH/CONTAINS confrontano un testo: senza, un `%` o un `_` arrivati
// da un query param diventavano jolly, e `CONTAINS "%"` selezionava tutte le righe.
func escapeLike(s string) string {
	r := strings.NewReplacer(likeEscape, likeEscape+likeEscape, "%", likeEscape+"%", "_", likeEscape+"_")
	return r.Replace(s)
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
		// LIKE/ILIKE espliciti ricevono un pattern: % e _ restano jolly, per scelta di chi scrive il
		// filtro. Per confrontare un testo arrivato dall'utente ci sono STARTSWITH/ENDSWITH/CONTAINS.
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
		return fmt.Sprintf("%s LIKE ? ESCAPE '!'", col), []any{escapeLike(s) + "%"}, nil

	case "ENDSWITH":
		s, ok := val.(string)
		if !ok {
			return "", nil, fmt.Errorf("operator ENDSWITH requires a string")
		}
		return fmt.Sprintf("%s LIKE ? ESCAPE '!'", col), []any{"%" + escapeLike(s)}, nil

	case "CONTAINS":
		s, ok := val.(string)
		if !ok {
			return "", nil, fmt.Errorf("operator CONTAINS requires a string")
		}
		return fmt.Sprintf("%s LIKE ? ESCAPE '!'", col), []any{"%" + escapeLike(s) + "%"}, nil

	case "IS NULL":
		return fmt.Sprintf("%s IS NULL", col), nil, nil

	case "IS NOT NULL":
		return fmt.Sprintf("%s IS NOT NULL", col), nil, nil

	default:
		return "", nil, fmt.Errorf("unsupported operator %q", op)
	}
}
