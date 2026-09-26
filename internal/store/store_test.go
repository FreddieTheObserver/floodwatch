package store

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/FreddieTheObserver/floodwatch/internal/source"
)

var testDSN string

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("floodwatch"),
		postgres.WithUsername("floodwatch"),
		postgres.WithPassword("floodwatch"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)
		os.Exit(1)
	}
	testDSN, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
		os.Exit(1)
	}
	if err := migrate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	if err := testcontainers.TerminateContainer(container); err != nil {
		fmt.Fprintf(os.Stderr, "terminate: %v\n", err)
	}
	os.Exit(code)
}

func migrate(ctx context.Context) error {
	s, err := New(ctx, Config{DSN: testDSN})
	if err != nil {
		return err
	}
	defer s.Close()
	return s.Migrate(ctx)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.Context(), Config{DSN: testDSN})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if _, err := s.pool.Exec(t.Context(), `TRUNCATE alert_states, subscriptions, recipients, water_readings, rain_readings, stations`); err != nil {
		t.Fatal(err)
	}
	return s
}

func ptr(f float64) *float64 { return &f }

var ict = time.FixedZone("ICT", 7*60*60)

func waterBatch(bank *float64, level float64, at time.Time) source.Batch {
	return source.Batch{
		Source: "thaiwater",
		Kind:   source.KindWater,
		Stations: []source.Station{{
			ExternalID: "1", Name: "Klong Ladprao Bang Bua Temple", NameTH: "คลองลาดพร้าว วัดบางบัว",
			District: "Bang Khen District", Lat: 13.87, Lng: 100.59, BankMSL: bank,
		}},
		Water: []source.WaterReading{{ExternalID: "1", ObservedAt: at, LevelMSL: level}},
	}
}

func TestSaveBatchIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	at := time.Date(2026, 9, 26, 13, 0, 0, 0, ict)

	first, err := s.SaveBatch(ctx, waterBatch(ptr(2.2), 2.82, at))
	if err != nil {
		t.Fatal(err)
	}
	if first.Stations != 1 || first.Water != 1 {
		t.Fatalf("first save = %+v", first)
	}

	again, err := s.SaveBatch(ctx, waterBatch(ptr(2.2), 2.82, at))
	if err != nil {
		t.Fatal(err)
	}
	if again.Water != 0 {
		t.Errorf("re-saving the same reading added %d rows", again.Water)
	}

	var stations, readings int
	s.pool.QueryRow(ctx, `SELECT count(*) FROM stations`).Scan(&stations)
	s.pool.QueryRow(ctx, `SELECT count(*) FROM water_readings`).Scan(&readings)
	if stations != 1 || readings != 1 {
		t.Errorf("stations=%d readings=%d, want 1/1", stations, readings)
	}
}

func TestSaveBatchKeepsLastKnownBank(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	at := time.Date(2026, 9, 26, 13, 0, 0, 0, ict)

	if _, err := s.SaveBatch(ctx, waterBatch(ptr(2.2), 2.82, at)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveBatch(ctx, waterBatch(nil, 2.9, at.Add(10*time.Minute))); err != nil {
		t.Fatal(err)
	}

	var bank *float64
	if err := s.pool.QueryRow(ctx, `SELECT bank_msl FROM stations`).Scan(&bank); err != nil {
		t.Fatal(err)
	}
	if bank == nil || *bank != 2.2 {
		t.Errorf("bank = %v, want the last known 2.2", bank)
	}
}

func TestSaveBatchRain(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	b := source.Batch{
		Source:   "bma",
		Kind:     source.KindRain,
		Stations: []source.Station{{ExternalID: "RF.TKU.01", Name: "Thung Khru District Office", Lat: 13.61135, Lng: 100.50878}},
		Rain: []source.RainReading{{
			ExternalID: "RF.TKU.01", ObservedAt: time.Date(2026, 9, 26, 13, 10, 0, 0, ict),
			Rain1h: ptr(1), Rain3h: ptr(4), Rain24h: ptr(92.5),
		}},
	}
	saved, err := s.SaveBatch(ctx, b)
	if err != nil || saved.Rain != 1 {
		t.Fatalf("save = %+v, %v", saved, err)
	}

	var nameTH, district *string
	if err := s.pool.QueryRow(ctx, `SELECT name_th, district FROM stations`).Scan(&nameTH, &district); err != nil {
		t.Fatal(err)
	}
	if nameTH != nil || district != nil {
		t.Errorf("empty strings were stored instead of NULL: %v %v", nameTH, district)
	}
}

func TestSaveBatchIsAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	b := waterBatch(ptr(2.2), 2.82, time.Date(2026, 9, 26, 13, 0, 0, 0, ict))
	b.Water = append(b.Water, source.WaterReading{ExternalID: "not-in-batch", ObservedAt: time.Now(), LevelMSL: 1})

	if _, err := s.SaveBatch(ctx, b); err == nil {
		t.Fatal("want an error for a reading of an unknown station")
	}
	var stations int
	s.pool.QueryRow(ctx, `SELECT count(*) FROM stations`).Scan(&stations)
	if stations != 0 {
		t.Errorf("a failed batch left %d stations behind", stations)
	}
}

// Every Down has to undo its Up exactly, or the Up after a rollback fails.
func TestMigrationsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	db := stdlib.OpenDBFromPool(s.pool)
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, sub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("up after down: %v", err)
	}
}
