package mq

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// ProcessIdempotent executes handler at most once per (consumerGroup, eventID) using mq_consumed_events.
func ProcessIdempotent(ctx context.Context, db *sql.DB, consumerGroup, eventID, topic string, fn func() error) (bool, error) {
	if db == nil {
		return true, fn()
	}
	var existing int
	err := db.QueryRowContext(ctx, "SELECT 1 FROM mq_consumed_events WHERE consumer_group=? AND event_id=?", consumerGroup, eventID).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err := fn(); err != nil {
		return false, err
	}
	_, err = db.ExecContext(ctx,
		"INSERT IGNORE INTO mq_consumed_events(consumer_group,event_id,topic,consumed_at) VALUES(?,?,?,?)",
		consumerGroup, eventID, topic, time.Now().Unix(),
	)
	return true, err
}

// KafkaConsumerGroup runs a kafka.Reader consumer group with retry + DLQ fallback.
type KafkaConsumerGroup struct {
	reader    *kafka.Reader
	dlq       Transport
	db        *sql.DB
	group     string
	topic     string
	handler   func(context.Context, Message) error
	stats     RelayStats
	stopOnce  sync.Once
	cancel    context.CancelFunc
	doneCh    chan struct{}
}

func StartKafkaConsumerGroup(ctx context.Context, brokers []string, group, topic string, db *sql.DB, dlq Transport, stats RelayStats, handler func(context.Context, Message) error) *KafkaConsumerGroup {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		GroupID:        group,
		Topic:          topic,
		MinBytes:       1,
		MaxBytes:       1 << 20,
		CommitInterval: time.Second,
		StartOffset:    kafka.FirstOffset,
	})
	cCtx, cancel := context.WithCancel(ctx)
	cg := &KafkaConsumerGroup{
		reader:  reader,
		dlq:     dlq,
		db:      db,
		group:   group,
		topic:   topic,
		handler: handler,
		stats:   stats,
		cancel:  cancel,
		doneCh:  make(chan struct{}),
	}
	go cg.run(cCtx)
	return cg
}

func (c *KafkaConsumerGroup) Stop() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		c.cancel()
		_ = c.reader.Close()
		<-c.doneCh
	})
}

func (c *KafkaConsumerGroup) run(ctx context.Context) {
	defer close(c.doneCh)
	for {
		m, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("kafka_fetch_error", "topic", c.topic, "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		eventID := string(m.Key)
		for _, h := range m.Headers {
			if h.Key == "event_id" && len(h.Value) > 0 {
				eventID = string(h.Value)
				break
			}
		}
		msg := Message{
			EventID: eventID,
			Topic:   c.topic,
			Key:     string(m.Key),
			Payload: m.Value,
		}
		var procErr error
		for attempt := 0; attempt < 3; attempt++ {
			_, procErr = ProcessIdempotent(ctx, c.db, c.group, eventID, c.topic, func() error {
				return c.handler(ctx, msg)
			})
			if procErr == nil {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(100*(1<<attempt)) * time.Millisecond):
			}
		}
		if procErr != nil && c.dlq != nil {
			dlqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			_ = c.dlq.Publish(dlqCtx, Message{
				EventID: eventID,
				Topic:   c.topic + ".dlq",
				Key:     string(m.Key),
				Payload: m.Value,
			})
			cancel()
			if c.stats != nil {
				c.stats.RecordOutboxDLQ()
			}
			slog.Error("kafka_consumer_routed_to_dlq", "topic", c.topic, "event_id", eventID, "error", procErr)
		}
		if err := c.reader.CommitMessages(ctx, m); err != nil && ctx.Err() == nil {
			slog.Warn("kafka_commit_failed", "topic", c.topic, "error", err)
		}
	}
}
