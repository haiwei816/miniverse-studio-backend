CREATE TABLE IF NOT EXISTS companions (
  player_id VARCHAR(64) PRIMARY KEY,
  id VARCHAR(64) NOT NULL UNIQUE,
  name VARCHAR(48) NOT NULL,
  style VARCHAR(32) NOT NULL,
  updated_at BIGINT NOT NULL
);
CREATE TABLE IF NOT EXISTS companion_memories (
  id VARCHAR(64) PRIMARY KEY,
  player_id VARCHAR(64) NOT NULL,
  content VARCHAR(1000) NOT NULL,
  created_at BIGINT NOT NULL,
  INDEX companion_memory_owner(player_id,created_at,id)
);
CREATE TABLE IF NOT EXISTS companion_contexts (
  player_id VARCHAR(64) PRIMARY KEY,
  game_id VARCHAR(64) NOT NULL,
  epoch BIGINT NOT NULL,
  scene_id VARCHAR(128) NOT NULL DEFAULT '',
  quest_stage VARCHAR(256) NOT NULL DEFAULT '',
  visible_targets JSON NOT NULL,
  updated_at BIGINT NOT NULL
);
