CREATE TABLE IF NOT EXISTS audits (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  admin_id VARCHAR(64),
  game_id VARCHAR(64),
  action VARCHAR(128),
  reason VARCHAR(256),
  details JSON,
  created_at BIGINT
);
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='audits' AND column_name='request_id')=0, 'ALTER TABLE audits ADD COLUMN request_id VARCHAR(64) GENERATED ALWAYS AS (LEFT(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(details, ''$.request_id'')), ''''), 64)) VIRTUAL', 'DO 0');
PREPARE perf_stmt FROM @ddl;
EXECUTE perf_stmt;
DEALLOCATE PREPARE perf_stmt;
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='audits' AND index_name='audits_game_request')=0, 'CREATE INDEX audits_game_request ON audits(game_id, request_id)', 'DO 0');
PREPARE perf_stmt FROM @ddl;
EXECUTE perf_stmt;
DEALLOCATE PREPARE perf_stmt;
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='ops_agent_runs' AND index_name='ops_agent_runs_created')=0, 'CREATE INDEX ops_agent_runs_created ON ops_agent_runs(created_at)', 'DO 0');
PREPARE perf_stmt FROM @ddl;
EXECUTE perf_stmt;
DEALLOCATE PREPARE perf_stmt;
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='ops_agent_steps' AND index_name='ops_agent_steps_created')=0, 'CREATE INDEX ops_agent_steps_created ON ops_agent_steps(created_at)', 'DO 0');
PREPARE perf_stmt FROM @ddl;
EXECUTE perf_stmt;
DEALLOCATE PREPARE perf_stmt;
