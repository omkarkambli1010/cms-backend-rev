// Package db is the only place SQL text is built, and the only thing it ever
// builds is `SELECT * FROM cms.<routine>($1, $2, ...)`. Every value travels as
// a bound parameter; the routine name is validated against a strict pattern
// and always comes from code, never from a request.
package db

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Row map[string]any

type DB struct{ Pool *pgxpool.Pool }

func New(ctx context.Context, url string, maxConns int32, statementTimeout time.Duration) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MaxConnIdleTime = 30 * time.Second
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.ConnConfig.RuntimeParams["application_name"] = "sbi-cms-backend"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprint(statementTimeout.Milliseconds())

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

func (d *DB) Close() { d.Pool.Close() }

// Ping reports whether the database answers.
func (d *DB) Ping(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return d.Pool.Ping(ctx) == nil
}

var routineName = regexp.MustCompile(`^cms\.[a-z_]+$`)

func statement(fn string, n int) (string, error) {
	if !routineName.MatchString(fn) {
		return "", fmt.Errorf("refusing to call a routine with an unexpected name: %q", fn)
	}
	ph := make([]string, n)
	for i := range ph {
		ph[i] = fmt.Sprintf("$%d", i+1)
	}
	return fmt.Sprintf("SELECT * FROM %s(%s)", fn, strings.Join(ph, ", ")), nil
}

// Querier is satisfied by the pool and by a transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Call runs a stored routine and returns its rows.
func (d *DB) Call(ctx context.Context, fn string, args ...any) ([]Row, error) {
	return Call(ctx, d.Pool, fn, args...)
}

// One is Call for routines that return a single row, or none.
//
// A routine declared RETURNS <composite> yields one row of all-NULL columns
// when it matched nothing, rather than zero rows; that is reported as nil so
// callers get "not found" instead of an object full of nulls.
func (d *DB) One(ctx context.Context, fn string, args ...any) (Row, error) {
	return One(ctx, d.Pool, fn, args...)
}

func Call(ctx context.Context, q Querier, fn string, args ...any) ([]Row, error) {
	sql, err := statement(fn, len(args))
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	out := make([]Row, 0)
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(Row, len(vals))
		for i, v := range vals {
			row[fields[i].Name] = convert(v, fields[i].DataTypeOID)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func One(ctx context.Context, q Querier, fn string, args ...any) (Row, error) {
	rows, err := Call(ctx, q, fn, args...)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	row := rows[0]
	for _, v := range row {
		if v != nil {
			return row, nil
		}
	}
	return nil, nil
}

// Scalar returns the single value of a one-column row (or nil).
func Scalar(row Row) any {
	for _, v := range row {
		return v
	}
	return nil
}

// Wire shapes the dashboard was written against: bigint travels as a string
// (as node-postgres sent it), dates as YYYY-MM-DD, timestamps as UTC ISO-8601
// with milliseconds, numeric as a string.
const (
	oidInt8      = 20
	oidInt8Array = 1016
	oidDate      = 1082
)

func convert(v any, oid uint32) any {
	switch x := v.(type) {
	case nil:
		return nil
	case int64:
		if oid == oidInt8 {
			return fmt.Sprint(x)
		}
		return x
	case time.Time:
		if oid == oidDate {
			return x.Format("2006-01-02")
		}
		return x.UTC().Format("2006-01-02T15:04:05.000Z")
	case pgtype.Numeric:
		if !x.Valid {
			return nil
		}
		b, err := x.MarshalJSON()
		if err != nil {
			return nil
		}
		return strings.Trim(string(b), `"`)
	case [16]byte:
		return fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:])
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			elem := uint32(0)
			if oid == oidInt8Array {
				elem = oidInt8
			}
			out[i] = convert(e, elem)
		}
		return out
	default:
		return v
	}
}
