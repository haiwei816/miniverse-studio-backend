CREATE TABLE IF NOT EXISTS ops_agent_config (
  id TINYINT PRIMARY KEY,
  protocol VARCHAR(32) NOT NULL DEFAULT '',
  url VARCHAR(512) NOT NULL DEFAULT '',
  model VARCHAR(128) NOT NULL DEFAULT '',
  api_key TEXT NOT NULL,
  updated_by VARCHAR(64) NOT NULL DEFAULT '',
  updated_at BIGINT NOT NULL DEFAULT 0
);
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='ops_agent_config' AND column_name='api_key_ciphertext')=0, 'ALTER TABLE ops_agent_config ADD COLUMN api_key_ciphertext TEXT NULL', 'DO 0');
PREPARE ops_sec_stmt FROM @ddl;
EXECUTE ops_sec_stmt;
DEALLOCATE PREPARE ops_sec_stmt;
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='ops_agent_config' AND column_name='key_encrypted')=0, 'ALTER TABLE ops_agent_config ADD COLUMN key_encrypted BOOLEAN NOT NULL DEFAULT FALSE', 'DO 0');
PREPARE ops_sec_stmt FROM @ddl;
EXECUTE ops_sec_stmt;
DEALLOCATE PREPARE ops_sec_stmt;
UPDATE ops_agent_config SET api_key='' WHERE api_key<>'';
