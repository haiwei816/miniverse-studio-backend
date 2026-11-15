-- Guard each ALTER because MySQL DDL implicitly commits and may be retried.
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='launch_grants' AND column_name='context_epoch')=0, 'ALTER TABLE launch_grants ADD COLUMN context_epoch BIGINT NOT NULL DEFAULT 0', 'DO 0');
PREPARE context_stmt FROM @ddl;
EXECUTE context_stmt;
DEALLOCATE PREPARE context_stmt;
SET @ddl = IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='game_sessions' AND column_name='context_epoch')=0, 'ALTER TABLE game_sessions ADD COLUMN context_epoch BIGINT NOT NULL DEFAULT 0', 'DO 0');
PREPARE context_stmt FROM @ddl;
EXECUTE context_stmt;
DEALLOCATE PREPARE context_stmt;
