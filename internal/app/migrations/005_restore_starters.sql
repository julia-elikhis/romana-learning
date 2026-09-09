-- Restore the built-in practice set without reviving deleted course questions.
UPDATE exercises SET status='published'
WHERE generator_version='starter-v1' AND material_id IS NULL AND status='deleted';
