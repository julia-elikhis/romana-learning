CREATE TABLE question_reports (
 id TEXT PRIMARY KEY,
 exercise_id TEXT NOT NULL REFERENCES exercises(id) ON DELETE CASCADE,
 reporter_id TEXT REFERENCES app_users(id) ON DELETE SET NULL,
 question_prompt TEXT NOT NULL,
 note TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 resolved_at TIMESTAMPTZ,
 resolved_by TEXT REFERENCES app_users(id) ON DELETE SET NULL
);
CREATE INDEX question_reports_status_created ON question_reports(status,created_at,id);
CREATE INDEX attempts_user_exercise ON attempts(user_id,exercise_id);
