-- 0034_feature_flags.down.sql
--
-- Reverses Faz 16 #173 feature_flags table.

DROP INDEX IF EXISTS idx_feature_flags_updated_at;
DROP TABLE IF EXISTS feature_flags;
