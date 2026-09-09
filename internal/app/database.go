package app

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
)

//go:embed migrations/002_library.sql
var libraryMigration string

//go:embed migrations/003_question_management.sql
var questionManagementMigration string

//go:embed migrations/004_github_accounts.sql
var githubAccountsMigration string

//go:embed migrations/005_restore_starters.sql
var restoreStartersMigration string

func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(726662031)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS attempts(id TEXT PRIMARY KEY,exercise_id TEXT NOT NULL,answer TEXT NOT NULL,correct BOOLEAN NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now());`); err != nil {
		return err
	}
	var installed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=2)`).Scan(&installed); err != nil {
		return err
	}
	if !installed {
		if _, err = tx.ExecContext(ctx, libraryMigration); err != nil {
			return err
		}
		for _, e := range legacyExercises {
			options, _ := json.Marshal(e.Options)
			answers, _ := json.Marshal([]string{e.Answer})
			if _, err = tx.ExecContext(ctx, `INSERT INTO exercises(id,kind,prompt,options,answers,explanation,status,generator_version) VALUES($1,'multiple_choice',$2,$3,$4,$5,'published','starter-v1') ON CONFLICT DO NOTHING`, e.ID, e.Prompt, string(options), string(answers), e.Explanation); err != nil {
				return err
			}
		}
		// Preserve grading snapshots for the earlier starter attempts too.
		if _, err = tx.ExecContext(ctx, `UPDATE attempts a SET correct_answer=e.answers->>0,explanation=e.explanation FROM exercises e WHERE a.exercise_id=e.id AND a.correct_answer=''`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(2)`); err != nil {
			return err
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=3)`).Scan(&installed); err != nil {
		return err
	}
	if !installed {
		if _, err = tx.ExecContext(ctx, questionManagementMigration); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(3)`); err != nil {
			return err
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=4)`).Scan(&installed); err != nil {
		return err
	}
	if !installed {
		if _, err = tx.ExecContext(ctx, githubAccountsMigration); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(4)`); err != nil {
			return err
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=5)`).Scan(&installed); err != nil {
		return err
	}
	if !installed {
		if _, err = tx.ExecContext(ctx, restoreStartersMigration); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(5)`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
