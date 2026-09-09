package config

import (
	"errors"
	"os"

	"github.com/jackc/pgx/v5"
)

// Database accepts the original DATABASE_URL or explicit libpq-style settings.
// Require a deliberate target rather than falling back to a local/default DB.
func Database() (*pgx.ConnConfig, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		for _, key := range []string{"PGHOST", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSSLMODE"} {
			if os.Getenv(key) == "" {
				return nil, errors.New("set DATABASE_URL or PGHOST, PGDATABASE, PGUSER, PGPASSWORD, and PGSSLMODE")
			}
		}
	}
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		// Driver errors can contain the input URL or password. Keep logs generic.
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	return cfg, nil
}
