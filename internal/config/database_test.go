package config

import (
	"testing"
)

func clearDatabaseEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"DATABASE_URL", "PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSSLMODE", "PGSSLROOTCERT", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(key, "")
	}
}

func TestExplicitDatabaseAndPasswordPreserved(t *testing.T) {
	clearDatabaseEnv(t)
	t.Setenv("PGHOST", "postgres.internal.example")
	t.Setenv("PGPORT", "5433")
	t.Setenv("PGDATABASE", "romanian_only")
	t.Setenv("PGUSER", "learner_app")
	t.Setenv("PGPASSWORD", "p@ss /?'=word")
	t.Setenv("PGSSLMODE", "verify-full")
	cfg, err := Database()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database != "romanian_only" || cfg.User != "learner_app" || cfg.Password != "p@ss /?'=word" || cfg.Port != 5433 {
		t.Fatal("explicit configuration changed")
	}
	if cfg.TLSConfig == nil || cfg.TLSConfig.InsecureSkipVerify {
		t.Fatal("verified TLS not enabled")
	}
}

func TestLocalURLCompatible(t *testing.T) {
	clearDatabaseEnv(t)
	t.Setenv("DATABASE_URL", "postgres://local:local@127.0.0.1:5432/romanian?sslmode=disable")
	cfg, err := Database()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database != "romanian" || cfg.TLSConfig != nil {
		t.Fatal("local URL changed")
	}
}

func TestMissingTargetAndInvalidConfigFailWithoutSecrets(t *testing.T) {
	clearDatabaseEnv(t)
	if _, err := Database(); err == nil {
		t.Fatal("missing database settings accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://private-password@[invalid")
	if _, err := Database(); err == nil || err.Error() != "invalid PostgreSQL configuration" {
		t.Fatal("invalid config error should be generic")
	}
}
