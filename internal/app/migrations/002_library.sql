CREATE TABLE IF NOT EXISTS materials (
 id TEXT PRIMARY KEY,
 root_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision > 0),
 title TEXT NOT NULL,
 filename TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('notes','teacher_corrections','homework','student_submission')),
 content_hash TEXT NOT NULL,
 object_key TEXT NOT NULL,
 extracted_text TEXT NOT NULL,
 reviewed BOOLEAN NOT NULL DEFAULT false,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(root_id,revision), UNIQUE(root_id,content_hash)
);
CREATE TABLE IF NOT EXISTS exercises (
 id TEXT PRIMARY KEY,
 material_id TEXT REFERENCES materials(id),
 kind TEXT NOT NULL CHECK(kind IN ('cloze','multiple_choice')),
 prompt TEXT NOT NULL,
 options JSONB NOT NULL DEFAULT '[]',
 answers JSONB NOT NULL,
 explanation TEXT NOT NULL,
 source_quote TEXT NOT NULL DEFAULT '',
 source_line INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL CHECK(status IN ('draft','published','rejected')),
 generator_version TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS exercises_material ON exercises(material_id);
CREATE INDEX IF NOT EXISTS exercises_status ON exercises(status);
ALTER TABLE attempts ADD COLUMN IF NOT EXISTS correct_answer TEXT NOT NULL DEFAULT '';
ALTER TABLE attempts ADD COLUMN IF NOT EXISTS explanation TEXT NOT NULL DEFAULT '';
ALTER TABLE attempts ADD COLUMN IF NOT EXISTS source_quote TEXT NOT NULL DEFAULT '';
