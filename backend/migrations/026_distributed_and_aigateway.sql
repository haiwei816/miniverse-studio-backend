CREATE TABLE IF NOT EXISTS outbox_events (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  event_id VARCHAR(64) NOT NULL,
  topic VARCHAR(128) NOT NULL,
  partition_key VARCHAR(128) NOT NULL DEFAULT '',
  payload JSON NOT NULL,
  status VARCHAR(24) NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at BIGINT NOT NULL DEFAULT 0,
  last_error VARCHAR(512) NOT NULL DEFAULT '',
  created_at BIGINT NOT NULL,
  dispatched_at BIGINT NULL,
  UNIQUE KEY outbox_event_id_uq (event_id),
  INDEX outbox_pending_idx (status, next_attempt_at, id),
  INDEX outbox_created_idx (created_at)
);

CREATE TABLE IF NOT EXISTS mq_consumed_events (
  consumer_group VARCHAR(64) NOT NULL,
  event_id VARCHAR(64) NOT NULL,
  topic VARCHAR(128) NOT NULL,
  consumed_at BIGINT NOT NULL,
  PRIMARY KEY (consumer_group, event_id),
  INDEX mq_consumed_at_idx (consumed_at)
);

CREATE TABLE IF NOT EXISTS batch_grant_jobs (
  id VARCHAR(64) PRIMARY KEY,
  game_id VARCHAR(64) NOT NULL,
  request_id VARCHAR(64) NOT NULL,
  item VARCHAR(64) NOT NULL,
  amount BIGINT NOT NULL,
  reason VARCHAR(256) NOT NULL,
  total_players INT NOT NULL DEFAULT 0,
  succeeded_players INT NOT NULL DEFAULT 0,
  failed_players INT NOT NULL DEFAULT 0,
  state VARCHAR(24) NOT NULL DEFAULT 'queued',
  last_error VARCHAR(512) NOT NULL DEFAULT '',
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL,
  INDEX batch_jobs_game_created (game_id, created_at),
  INDEX batch_jobs_request (game_id, request_id)
);

CREATE TABLE IF NOT EXISTS ai_gateway_audits (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  trace_id VARCHAR(64) NOT NULL DEFAULT '',
  source VARCHAR(32) NOT NULL,
  game_id VARCHAR(64) NOT NULL DEFAULT '',
  actor_id VARCHAR(64) NOT NULL DEFAULT '',
  hook_stage VARCHAR(32) NOT NULL,
  decision VARCHAR(24) NOT NULL,
  reason_code VARCHAR(64) NOT NULL DEFAULT '',
  pii_redacted_count INT NOT NULL DEFAULT 0,
  detail JSON NULL,
  created_at BIGINT NOT NULL,
  INDEX ai_gw_game_created (game_id, created_at),
  INDEX ai_gw_created (created_at),
  INDEX ai_gw_trace (trace_id)
);
