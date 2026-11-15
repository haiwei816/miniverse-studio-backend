-- MySQL DDL commits implicitly. Check each index so interruption can be retried.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='sessions' AND index_name='sessions_expiry')=0, 'CREATE INDEX sessions_expiry ON sessions(expires_at)', 'DO 0');
PREPARE expiry_stmt FROM @ddl;
EXECUTE expiry_stmt;
DEALLOCATE PREPARE expiry_stmt;
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='gm_sessions' AND index_name='gm_sessions_expiry')=0, 'CREATE INDEX gm_sessions_expiry ON gm_sessions(expires_at)', 'DO 0');
PREPARE expiry_stmt FROM @ddl;
EXECUTE expiry_stmt;
DEALLOCATE PREPARE expiry_stmt;
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='platform_nonces' AND index_name='platform_nonces_expiry')=0, 'CREATE INDEX platform_nonces_expiry ON platform_nonces(expires_at)', 'DO 0');
PREPARE expiry_stmt FROM @ddl;
EXECUTE expiry_stmt;
DEALLOCATE PREPARE expiry_stmt;
