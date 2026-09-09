CREATE TABLE app_users (
 id TEXT PRIMARY KEY,
 github_id BIGINT UNIQUE,
 login TEXT NOT NULL,
 name TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO app_users(id,login,name) VALUES('legacy-local-owner','local-owner','Local learner');
ALTER TABLE materials ADD COLUMN owner_id TEXT REFERENCES app_users(id);
UPDATE materials SET owner_id='legacy-local-owner';
ALTER TABLE materials ALTER COLUMN owner_id SET NOT NULL;
CREATE INDEX materials_owner ON materials(owner_id);

ALTER TABLE attempts ADD COLUMN user_id TEXT REFERENCES app_users(id);
ALTER TABLE attempts ADD COLUMN question_prompt TEXT NOT NULL DEFAULT '';
UPDATE attempts SET user_id='legacy-local-owner';
UPDATE attempts a SET question_prompt=e.prompt FROM exercises e WHERE e.id=a.exercise_id;
ALTER TABLE attempts ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE attempts DROP CONSTRAINT attempts_pkey;
ALTER TABLE attempts ADD PRIMARY KEY(user_id,id);
CREATE INDEX attempts_user_created ON attempts(user_id,created_at DESC);

CREATE TABLE auth_sessions (
 token_hash TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES app_users(id) ON DELETE CASCADE,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX auth_sessions_expiry ON auth_sessions(expires_at);
CREATE TABLE oauth_states (
 state_hash TEXT PRIMARY KEY,
 verifier TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL
);
