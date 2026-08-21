// Package locker provides a SQL-backed implementation of the neutral
// go-core-app/lock.Locker primitive, built on bun. Locks are TTL lease rows in a
// dedicated table; mutual exclusion is achieved with a single conditional upsert
// (insert, or steal only when the existing lease is expired). It is
// dialect-agnostic (PostgreSQL, SQLite, MySQL) and does not depend on gocron.
package locker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app/lock"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
)

// DefaultTTL bounds how long a lease is held before another owner may steal it.
// The lock is a dispatch-dedup optimization (correctness lives in DB claiming),
// so a modest TTL with a single attempt is intended.
const DefaultTTL = 30 * time.Second

// defaultRetryDelay is the wait between acquisition attempts when the caller
// asked to block (Tries > 1) but did not set an explicit RetryDelay.
const defaultRetryDelay = 100 * time.Millisecond

// lockRow is the lease table. Column lock_key avoids the reserved word "key".
type lockRow struct {
	bun.BaseModel `bun:"table:scheduler_locks"`

	Key       string    `bun:"lock_key,pk"`
	Owner     string    `bun:"owner,notnull"`
	ExpiresAt time.Time `bun:"expires_at,notnull"`
}

type sqlLocker struct {
	db  *bun.DB
	ttl time.Duration
}

// New returns a SQL-backed lock.Locker over the given bun.DB.
func New(db *bun.DB) lock.Locker {
	return &sqlLocker{db: db, ttl: DefaultTTL}
}

// EnsureTable creates the scheduler_locks table if it does not exist. Apps may
// call it at startup instead of managing the migration by hand.
func EnsureTable(ctx context.Context, db *bun.DB) error {
	_, err := db.NewCreateTable().Model((*lockRow)(nil)).IfNotExists().Exec(ctx)
	return err
}

// Acquire honours the neutral AcquireOption set: without options it makes a
// single conditional-upsert attempt (dispatch-dedup); with Tries > 1 it retries
// on contention (RetryDelay between attempts) until it succeeds, the attempts are
// exhausted, or the context is done. Expiry overrides the lease TTL.
func (l *sqlLocker) Acquire(ctx context.Context, key string, opts ...lock.AcquireOption) (lock.Handle, error) {
	cfg := lock.ResolveAcquireConfig(opts...)
	ttl := l.ttl
	if cfg.Expiry > 0 {
		ttl = cfg.Expiry
	}
	tries := max(cfg.Tries, 1)
	delay := cfg.RetryDelay
	if delay <= 0 {
		delay = defaultRetryDelay
	}

	for attempt := 0; ; attempt++ {
		h, err := l.tryAcquire(ctx, key, ttl)
		if err == nil {
			return h, nil
		}
		if !errors.Is(err, lock.ErrNotAcquired) {
			return nil, err
		}
		if attempt+1 >= tries {
			return nil, lock.ErrNotAcquired
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// tryAcquire inserts the lease, or on primary-key conflict updates it only when
// the existing lease is expired. RowsAffected == 0 means the key is held by a
// live owner → not acquired.
func (l *sqlLocker) tryAcquire(ctx context.Context, key string, ttl time.Duration) (lock.Handle, error) {
	token, err := randToken()
	if err != nil {
		return nil, fmt.Errorf("sql lock acquire %q: %w", key, err)
	}
	now := time.Now()
	row := &lockRow{Key: key, Owner: token, ExpiresAt: now.Add(ttl)}

	q := l.db.NewInsert().Model(row)
	if l.db.Dialect().Name() == dialect.MySQL {
		q = q.On("DUPLICATE KEY UPDATE owner = IF(expires_at <= ?, VALUES(owner), owner), "+
			"expires_at = IF(expires_at <= ?, VALUES(expires_at), expires_at)", now, now)
	} else { // PG, SQLite
		q = q.On("CONFLICT (lock_key) DO UPDATE").
			Set("owner = EXCLUDED.owner").
			Set("expires_at = EXCLUDED.expires_at").
			Where("expires_at <= ?", now)
	}

	res, err := q.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("sql lock acquire %q: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("sql lock acquire %q: %w", key, err)
	}
	if n == 0 {
		return nil, lock.ErrNotAcquired
	}
	return &sqlHandle{db: l.db, key: key, token: token, ttl: ttl}, nil
}

type sqlHandle struct {
	db    *bun.DB
	key   string
	token string
	ttl   time.Duration
}

// Release deletes the lease only if this owner still holds it.
func (h *sqlHandle) Release(ctx context.Context) error {
	_, err := h.db.NewDelete().Model((*lockRow)(nil)).
		Where("lock_key = ?", h.key).
		Where("owner = ?", h.token).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("sql lock release %q: %w", h.key, err)
	}
	return nil
}

// Extend renews the lease TTL only if this owner still holds it. A lost lease
// (stolen after expiry, or already released) updates no row and is surfaced as
// lock.ErrLockLost.
func (h *sqlHandle) Extend(ctx context.Context) error {
	res, err := h.db.NewUpdate().Model((*lockRow)(nil)).
		Set("expires_at = ?", time.Now().Add(h.ttl)).
		Where("lock_key = ?", h.key).
		Where("owner = ?", h.token).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("sql lock extend %q: %w", h.key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sql lock extend %q: %w", h.key, err)
	}
	if n == 0 {
		return fmt.Errorf("sql lock extend %q: %w", h.key, lock.ErrLockLost)
	}
	return nil
}

func randToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
