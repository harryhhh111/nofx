-- Fix AI model and trader primary keys that contain "/".
--
-- Background:
--   AIModel IDs are built as {userID}_{provider}_{customModelName}.
--   When customModelName contained a namespace prefix like "qwen/qwen3.5-plus",
--   the slash leaked into the primary key, and because trader IDs embed the
--   AIModel ID, it also leaked into trader IDs. This breaks Gin routing for
--   endpoints such as POST /api/traders/:id/start.
--
--   After the code change, buildModelID only uses the part after the last "/"
--   (so "qwen/qwen3.5-plus-20260420" becomes "qwen3.5-plus-20260420" in the
--   key). This migration aligns existing data with that logic.
--
-- How to run (review first, then execute in a transaction):
--   psql "postgresql://USER:PASS@HOST:PORT/DB?sslmode=disable" -f scripts/fix_slash_in_trader_ids.sql
--
-- The script is idempotent: running it again will report 0 rows changed.

\echo '=== AI models with slash in id ==='
SELECT id, provider, custom_model_name
FROM ai_models
WHERE id LIKE '%/%'
ORDER BY id;

\echo '=== Traders with slash in id ==='
SELECT id, name, ai_model_id
FROM traders
WHERE id LIKE '%/%'
ORDER BY id;

BEGIN;

-- Build a fixed mapping from the original ai_model ids. We use a temporary
-- table so the mapping survives the UPDATE on ai_models itself.
CREATE TEMP TABLE _slash_model_fix ON COMMIT DROP AS
SELECT
    id AS old_id,
    regexp_replace(id, '^([^_]+_[^_]+)_(.+)$', '\1')
    || '_'
    || split_part(regexp_replace(id, '^([^_]+_[^_]+)_(.+)$', '\2'), '/', array_length(string_to_array(regexp_replace(id, '^([^_]+_[^_]+)_(.+)$', '\2'), '/'), 1)) AS new_id
FROM ai_models
WHERE id LIKE '%/%';

-- Also build the trader id mapping up front, before we modify any table.
CREATE TEMP TABLE _slash_trader_fix ON COMMIT DROP AS
SELECT
    t.id AS old_trader_id,
    replace(t.id, m.old_id, m.new_id) AS new_trader_id
FROM traders t
JOIN _slash_model_fix m ON t.id LIKE '%' || m.old_id || '%'
WHERE t.id LIKE '%/%';

\echo '=== Planned AI model id changes ==='
SELECT * FROM _slash_model_fix ORDER BY old_id;

\echo '=== Planned trader id changes ==='
SELECT * FROM _slash_trader_fix ORDER BY old_trader_id;

-- 1. Rename the AI model records.
UPDATE ai_models m
SET id = f.new_id
FROM _slash_model_fix f
WHERE m.id = f.old_id
  AND NOT EXISTS (SELECT 1 FROM ai_models ex WHERE ex.id = f.new_id AND ex.id != m.id);

-- 2. Update traders.ai_model_id to point to the renamed AI models.
UPDATE traders t
SET ai_model_id = f.new_id
FROM _slash_model_fix f
WHERE t.ai_model_id = f.old_id;

-- 3. Rename the traders themselves.
UPDATE traders t
SET id = f.new_trader_id
FROM _slash_trader_fix f
WHERE t.id = f.old_trader_id
  AND NOT EXISTS (SELECT 1 FROM traders ex WHERE ex.id = f.new_trader_id AND ex.id != t.id);

-- 4. Cascade the trader id change to all known related tables.
UPDATE trader_positions          SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE trader_orders             SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE trader_fills              SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE trader_equity_snapshots   SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE decision_records          SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE execution_analytics       SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE signal_calibration_samples SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE bb_macd_signals           SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE ai_charges                SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE trade_memories            SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;
UPDATE grid_configs              SET trader_id = f.new_trader_id FROM _slash_trader_fix f WHERE trader_id = f.old_trader_id;

COMMIT;

\echo '=== Remaining AI models with slash in id (should be 0) ==='
SELECT COUNT(*) FROM ai_models WHERE id LIKE '%/%';

\echo '=== Remaining traders with slash in id (should be 0) ==='
SELECT COUNT(*) FROM traders WHERE id LIKE '%/%';

\echo '=== Renamed AI model ids ==='
SELECT id, provider, custom_model_name
FROM ai_models
WHERE id IN (SELECT new_id FROM _slash_model_fix)
ORDER BY id;

\echo '=== Renamed trader ids ==='
SELECT id, name, ai_model_id
FROM traders
WHERE id IN (SELECT new_trader_id FROM _slash_trader_fix)
ORDER BY id;
