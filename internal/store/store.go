package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
	"github.com/FreddieTheObserver/floodwatch/internal/store/gen"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const defaultMaxConns = 4

type Config struct {
	DSN      string
	MaxConns int32
}

type Store struct {
	*gen.Queries

	pool *pgxpool.Pool
}

// New opens the pool and verifies it can reach the database.
func New(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.MaxConns <= 0 {
		cfg.MaxConns = defaultMaxConns
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{Queries: gen.New(pool), pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Migrate applies the embedded migrations. goose needs database/sql, so it
// borrows the existing pool rather than configuring a second driver.
func (s *Store) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}
	db := stdlib.OpenDBFromPool(s.pool)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, sub)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// Saved counts what a SaveBatch actually added. Readings already stored from an
// earlier poll are not counted.
type Saved struct {
	Stations int
	Water    int64
	Rain     int64
}

// SaveBatch stores one fetch atomically, so a batch is either fully visible or
// not at all.
func (s *Store) SaveBatch(ctx context.Context, b source.Batch) (Saved, error) {
	var saved Saved
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		saved = Saved{}
		q := s.WithTx(tx)

		ids := make(map[string]int64, len(b.Stations))
		for _, st := range b.Stations {
			id, err := q.UpsertStation(ctx, gen.UpsertStationParams{
				Source:     b.Source,
				Kind:       string(b.Kind),
				ExternalID: st.ExternalID,
				Name:       st.Name,
				NameTh:     optional(st.NameTH),
				District:   optional(st.District),
				Lat:        st.Lat,
				Lng:        st.Lng,
				BankMsl:    st.BankMSL,
				Agency:     optional(st.Agency),
			})
			if err != nil {
				return fmt.Errorf("upsert station %s/%s/%s: %w", b.Source, b.Kind, st.ExternalID, err)
			}
			ids[st.ExternalID] = id
		}
		saved.Stations = len(ids)

		stationID := func(externalID string) (int64, error) {
			id, ok := ids[externalID]
			if !ok {
				return 0, fmt.Errorf("reading for station %q, which is not in the batch", externalID)
			}
			return id, nil
		}

		for _, r := range b.Water {
			id, err := stationID(r.ExternalID)
			if err != nil {
				return err
			}
			n, err := q.InsertWaterReading(ctx, gen.InsertWaterReadingParams{
				StationID:  id,
				ObservedAt: r.ObservedAt,
				LevelMsl:   r.LevelMSL,
			})
			if err != nil {
				return fmt.Errorf("insert water reading for %s: %w", r.ExternalID, err)
			}
			saved.Water += n
		}

		for _, r := range b.Rain {
			id, err := stationID(r.ExternalID)
			if err != nil {
				return err
			}
			n, err := q.InsertRainReading(ctx, gen.InsertRainReadingParams{
				StationID:  id,
				ObservedAt: r.ObservedAt,
				Rain1hMm:   r.Rain1h,
				Rain3hMm:   r.Rain3h,
				Rain24hMm:  r.Rain24h,
			})
			if err != nil {
				return fmt.Errorf("insert rain reading for %s: %w", r.ExternalID, err)
			}
			saved.Rain += n
		}
		return nil
	})
	return saved, err
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
