CREATE TABLE IF NOT EXISTS items(game_id VARCHAR(64),id VARCHAR(64),name VARCHAR(128),PRIMARY KEY(game_id,id));
INSERT IGNORE INTO items VALUES('sky-island','wood','木材');
CREATE TABLE IF NOT EXISTS grants(id VARCHAR(64) PRIMARY KEY,game_id VARCHAR(64),player_id VARCHAR(64),business_key VARCHAR(192),item VARCHAR(64),amount BIGINT NOT NULL,created_at BIGINT,UNIQUE KEY business(game_id,player_id,business_key));
CREATE TABLE IF NOT EXISTS mail(id VARCHAR(64) PRIMARY KEY,game_id VARCHAR(64),player_id VARCHAR(64),grant_id VARCHAR(64) UNIQUE,title VARCHAR(128),expires_at BIGINT NOT NULL,claimed BOOLEAN NOT NULL DEFAULT FALSE);
CREATE TABLE IF NOT EXISTS inventory(game_id VARCHAR(64),player_id VARCHAR(64),item VARCHAR(64),amount BIGINT NOT NULL,PRIMARY KEY(game_id,player_id,item));
