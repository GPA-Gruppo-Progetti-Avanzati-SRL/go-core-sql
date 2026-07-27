package locker

import (
	core "github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app"
	"github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app/lock"
)

// Module registers the SQL-backed lock.Locker in the fx application. It is
// modes-only: it consumes the *bun.DB provided by the app (coresql.NewService),
// so no extra config is required. The app is responsible for the scheduler_locks
// table (see EnsureTable).
//
//	batch.Module(&cfg.Batch, batch.WithLocker(locker.Module), ...)
func Module(modes ...string) {
	core.ProvideAs[lock.Locker](New, modes...)
}
