package coresql

import (
	core "github.com/GPA-Gruppo-Progetti-Avanzati-SRL/go-core-app"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"
)

// Option configura Module.
type Option func(*moduleOptions)

type moduleOptions struct {
	modes []string
}

// WithModes limita la registrazione ai core.Mode indicati (nessuno = sempre).
func WithModes(modes ...string) Option {
	return func(o *moduleOptions) { o.modes = modes }
}

// dialectSource porta il dialect dal Module al costruttore: un'interfaccia non
// si può supplire direttamente a fx, quindi viaggia dentro una struct.
type dialectSource struct {
	dialect schema.Dialect
}

// Module wira il servizio SQL nell'applicazione fx: supplisce la Config e
// fornisce *Service — unico handle SQL dell'applicazione — più il *bun.DB
// sottostante, per i consumatori che costruiscono query bun native o DDL
// (es. locker.New, EnsureTable).
//
// È l'unico entry-point: il costruttore non è esportato e l'app non deve fare
// core.Supply/core.Provide a mano.
//
// Il dialect è un parametro perché è una scelta compile-time: l'app importa il
// package del dialect corrispondente al proprio driver, uno tra:
//   - github.com/uptrace/bun/dialect/pgdialect    → pgdialect.New()
//   - github.com/uptrace/bun/dialect/mysqldialect → mysqldialect.New()
//   - github.com/uptrace/bun/dialect/sqlitedialect → sqlitedialect.New()
//
// Esempio:
//
//	coresql.Module(&cfg.Sql, pgdialect.New())
//	coresql.Module(&cfg.Sql, pgdialect.New(), coresql.WithModes(engine.Api, engine.Batch))
func Module(cfg *Config, dialect schema.Dialect, opts ...Option) {
	var o moduleOptions
	for _, opt := range opts {
		opt(&o)
	}

	core.Module("sql", func() {
		core.Supply(cfg, o.modes...)
		core.Supply(dialectSource{dialect: dialect}, o.modes...)
		core.Provide(newService, o.modes...)
		core.Provide(serviceDB, o.modes...)
	})
}

// serviceDB espone il *bun.DB del Service nel grafo fx, così i componenti che
// lavorano con bun nativo (locker, DDL, query builder) non devono passare dal
// Service.
func serviceDB(s *Service) *bun.DB { return s.DB() }
