CREATE TABLE IF NOT EXISTS rule_boards(game_id VARCHAR(64),board_id VARCHAR(64),season VARCHAR(64),rule_version VARCHAR(32),PRIMARY KEY(game_id,board_id,season));
INSERT IGNORE INTO boards VALUES('card-game','verified','sandbox','max',1,4102444800,FALSE,FALSE,'gem',3);
INSERT IGNORE INTO rule_boards VALUES('card-game','verified','sandbox','card-v1');
CREATE TABLE IF NOT EXISTS replays(game_id VARCHAR(64),player_id VARCHAR(64),request_id VARCHAR(128),payload_hash CHAR(64),rule_version VARCHAR(32),seed BIGINT,actions JSON,score BIGINT,state_hash VARCHAR(32),PRIMARY KEY(game_id,player_id,request_id));
