CREATE TABLE IF NOT EXISTS platform_identities(channel VARCHAR(16),app_id VARCHAR(64),subject VARCHAR(128),player_id VARCHAR(64),PRIMARY KEY(channel,app_id,subject),INDEX(player_id));
CREATE TABLE IF NOT EXISTS platform_nonces(nonce VARCHAR(64) PRIMARY KEY,expires_at BIGINT);
