CREATE TABLE IF NOT EXISTS ops_agent_runs (
  id VARCHAR(64) PRIMARY KEY,
  game_id VARCHAR(64) NOT NULL,
  admin_id VARCHAR(64) NOT NULL,
  question VARCHAR(1000) NOT NULL,
  mode VARCHAR(32) NOT NULL,
  model VARCHAR(128) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL,
  summary TEXT NOT NULL,
  root_cause TEXT NOT NULL,
  recommendation TEXT NOT NULL,
  proposal_request_id VARCHAR(64) NOT NULL DEFAULT '',
  steps_count INT NOT NULL DEFAULT 0,
  prompt_tokens INT NOT NULL DEFAULT 0,
  completion_tokens INT NOT NULL DEFAULT 0,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL,
  INDEX ops_agent_runs_game_created (game_id, created_at)
);
CREATE TABLE IF NOT EXISTS ops_agent_steps (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  run_id VARCHAR(64) NOT NULL,
  step_index INT NOT NULL,
  tool_name VARCHAR(64) NOT NULL,
  arguments_json TEXT NOT NULL,
  result_summary TEXT NOT NULL,
  is_error BOOLEAN NOT NULL DEFAULT FALSE,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL,
  INDEX ops_agent_steps_run (run_id, step_index)
);
