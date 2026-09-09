ALTER TABLE exercises DROP CONSTRAINT exercises_status_check;
ALTER TABLE exercises ADD CONSTRAINT exercises_status_check
 CHECK(status IN ('draft','published','rejected','deleted'));

-- Keep the historical starter records for saved answers, but retire them from practice.
UPDATE exercises SET status='deleted' WHERE generator_version='starter-v1' AND material_id IS NULL;
