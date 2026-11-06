INSERT IGNORE INTO games(id,name) VALUES ('card-game','卡牌挑战');
INSERT IGNORE INTO items VALUES('card-game','gem','宝石');
INSERT IGNORE INTO gm_access VALUES('admin','card-game'),('operator','card-game'),('reviewer','card-game');
INSERT IGNORE INTO activities VALUES('sky-island','build-challenge',1,'{"id":"build-challenge","version":1,"kind":"cumulative","event_type":"building_upgraded","target":3,"starts_at":1,"ends_at":4102444800,"client_allowed":true,"item":"wood","reward_amount":5}',TRUE);
INSERT IGNORE INTO activities VALUES('card-game','match-challenge',1,'{"id":"match-challenge","version":1,"kind":"cumulative","event_type":"match_completed","target":2,"starts_at":1,"ends_at":4102444800,"client_allowed":true,"item":"gem","reward_amount":3}',TRUE);
INSERT IGNORE INTO boards VALUES('sky-island','prosperity','sandbox','max',1,4102444800,FALSE,FALSE,'wood',5);
INSERT IGNORE INTO boards VALUES('card-game','weekly','sandbox','max',1,4102444800,FALSE,FALSE,'gem',3);
