CREATE TABLE IF NOT EXISTS match_tickets(
  id VARCHAR(64) PRIMARY KEY,
  game_id VARCHAR(64) NOT NULL,
  player_id VARCHAR(64) NOT NULL,
  board_id VARCHAR(64) NOT NULL DEFAULT '',
  season VARCHAR(64) NOT NULL DEFAULT '',
  rule_version VARCHAR(32) NOT NULL,
  seed BIGINT NOT NULL,
  issued_at BIGINT NOT NULL,
  expires_at BIGINT NOT NULL,
  consumed_at BIGINT NULL,
  request_id VARCHAR(128) NOT NULL DEFAULT '',
  INDEX match_tickets_player(game_id, player_id, issued_at),
  INDEX match_tickets_expires(expires_at)
);
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='rule_boards' AND column_name='require_ticket')=0,
  'ALTER TABLE rule_boards ADD COLUMN require_ticket BOOLEAN NOT NULL DEFAULT FALSE',
  'DO 0');
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
INSERT IGNORE INTO boards VALUES('card-game','ranked','sandbox','max',1,4102444800,FALSE,FALSE,'gem',5);
INSERT IGNORE INTO rule_boards(game_id,board_id,season,rule_version,require_ticket) VALUES('card-game','ranked','sandbox','card-v1',TRUE);
CREATE TABLE IF NOT EXISTS player_bans(
  game_id VARCHAR(64) NOT NULL,
  player_id VARCHAR(64) NOT NULL,
  reason VARCHAR(256) NOT NULL,
  banned_by VARCHAR(64) NOT NULL,
  banned_at BIGINT NOT NULL,
  expires_at BIGINT NULL,
  PRIMARY KEY(game_id, player_id),
  INDEX player_bans_player(player_id, expires_at),
  INDEX player_bans_expires(expires_at)
);
