package service

import (
	"context"
	"miniverse/backend/internal/lock"
	"miniverse/backend/internal/mq"
	"miniverse/backend/internal/redisx"
	"strings"
)

func (s *Service) Initialize() error {
	s.ConcurrentDiagnostics = true
	s.EnforceBans = true
	if s.PersistOpsEvents {
		s.startOpsWriter()
	}
	if strings.TrimSpace(s.RedisAddr) != "" {
		nodeID := random()[:12]
		s.Redis = redisx.Open(s.RedisAddr, nodeID)
		if s.Redis != nil && s.Redis.Raw() != nil {
			s.LockStore = lock.NewRedisStore(s.Redis.Raw())
			if s.redisRateLimiter != nil {
				s.redisRateLimiter.SetRedis(s.Redis.Raw())
			}
			s.stopInvalidationSub = s.Redis.StartInvalidationSubscriber(context.Background(), func(ev redisx.InvalidationEvent) {
				switch ev.Kind {
				case "game":
					if s.gameCache != nil {
						if ev.GameID == "" {
							s.gameCache.Clear()
						} else {
							s.gameCache.Delete(ev.GameID)
						}
					}
				case "activity":
					if s.activitiesCache != nil {
						if ev.GameID == "" {
							s.activitiesCache.Clear()
						} else {
							s.activitiesCache.Delete(ev.GameID)
						}
					}
				case "ban":
					if s.banCache != nil && ev.PlayerID != "" {
						s.banCache.Delete(ev.PlayerID)
					}
				}
			})
		}
	}
	s.initDistributedWorkers()
	if err := s.seedGM(); err != nil {
		return err
	}
	if err := s.seedPlayerPlatform(); err != nil {
		return err
	}
	s.loadPersistedOpsAgentConfig()
	return nil
}

func (s *Service) initDistributedWorkers() {
	if s.DB == nil {
		return
	}
	if len(s.KafkaBrokers) > 0 {
		kt := mq.NewKafkaTransport(s.KafkaBrokers)
		s.MQTransport = kt
		s.OutboxRelay = mq.NewOutboxRelay(s.DB, kt, s.Telemetry)
		s.OutboxRelay.Start()
		cg := mq.StartKafkaConsumerGroup(context.Background(), s.KafkaBrokers, "miniverse-reward-workers", mq.TopicRewardBatch, s.DB, kt, s.Telemetry, s.handleRewardBatchMessage)
		s.kafkaConsumers = append(s.kafkaConsumers, cg)
	} else {
		mt := mq.NewInMemoryTransport()
		mt.Register(mq.TopicRewardBatch, s.handleRewardBatchMessage)
		s.MQTransport = mt
		s.OutboxRelay = mq.NewOutboxRelay(s.DB, mt, s.Telemetry)
		s.OutboxRelay.Start()
	}
	s.stopSeasonSettler = s.startSeasonAutoSettler()
}
