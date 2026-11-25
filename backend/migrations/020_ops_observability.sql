CREATE TABLE IF NOT EXISTS ops_events (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  request_id VARCHAR(64) NOT NULL DEFAULT '',
  game_id VARCHAR(64) NOT NULL DEFAULT '',
  player_id VARCHAR(64) NOT NULL DEFAULT '',
  category VARCHAR(32) NOT NULL,
  method VARCHAR(16) NOT NULL DEFAULT '',
  path VARCHAR(256) NOT NULL DEFAULT '',
  status_code INT NOT NULL DEFAULT 0,
  error_code VARCHAR(64) NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  detail JSON NULL,
  created_at BIGINT NOT NULL,
  INDEX ops_events_game_created (game_id, created_at),
  INDEX ops_events_request (request_id),
  INDEX ops_events_player_created (game_id, player_id, created_at),
  INDEX ops_events_created (created_at)
);
