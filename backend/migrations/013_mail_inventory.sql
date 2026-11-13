CREATE TABLE IF NOT EXISTS mail_states (
  mail_id VARCHAR(64) PRIMARY KEY,
  read_at BIGINT NOT NULL DEFAULT 0,
  deleted_at BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS inventory_requests (
  game_id VARCHAR(64) NOT NULL,
  player_id VARCHAR(64) NOT NULL,
  request_id VARCHAR(128) NOT NULL,
  item VARCHAR(64) NOT NULL,
  amount BIGINT NOT NULL,
  result_balance BIGINT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY(game_id,player_id,request_id)
);
CREATE TABLE IF NOT EXISTS inventory_ledger (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  game_id VARCHAR(64) NOT NULL,
  player_id VARCHAR(64) NOT NULL,
  item VARCHAR(64) NOT NULL,
  delta BIGINT NOT NULL,
  balance BIGINT NOT NULL,
  business_key VARCHAR(192) NOT NULL,
  reason VARCHAR(64) NOT NULL,
  created_at BIGINT NOT NULL,
  UNIQUE KEY inventory_business(game_id,player_id,business_key),
  INDEX inventory_history(game_id,player_id,id)
);
INSERT IGNORE INTO inventory_ledger(game_id,player_id,item,delta,balance,business_key,reason,created_at)
SELECT game_id,player_id,item,amount,amount,CONCAT('opening:',item),'migration_opening',UNIX_TIMESTAMP()
FROM inventory WHERE amount>0;
