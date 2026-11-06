CREATE TABLE IF NOT EXISTS gm_requests(game_id VARCHAR(64),id VARCHAR(64),kind VARCHAR(16),creator VARCHAR(64),reviewer VARCHAR(64) DEFAULT '',state VARCHAR(16),reason VARCHAR(256),payload JSON,created_at BIGINT,PRIMARY KEY(game_id,id));
CREATE TABLE IF NOT EXISTS activity_versions(game_id VARCHAR(64),id VARCHAR(64),version INT,config JSON,PRIMARY KEY(game_id,id,version));
CREATE TABLE IF NOT EXISTS announcements(game_id VARCHAR(64),id VARCHAR(64),body VARCHAR(2000),expires_at BIGINT,PRIMARY KEY(game_id,id));
