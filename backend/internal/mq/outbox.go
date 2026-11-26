// Package mq provides a Transactional Outbox relay, Kafka/Redpanda transport,
// idempotent consumer deduplication, exponential retry, and DLQ handling.
package mq

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

const (
	TopicRewardBatch = "miniverse.reward.batch"
	TopicOpsEvents   = "miniverse.ops.events"
	maxRelayAttempts = 5
)

// Message represents a queued domain event.
type Message struct {
	EventID string
	Topic   string
	Key     string
	Payload json.RawMessage
}

// Transport abstracts Kafka publishing so unit tests can run deterministically without a broker.
type Transport interface {
	Publish(ctx context.Context, msg Message) error
	Close() error
}

// KafkaTransport publishes messages to Redpanda/Kafka with RequireAll acks and hash partitioning.
type KafkaTransport struct {
	mu      sync.Mutex
	brokers []string
	writers map[string]*kafka.Writer
}

func NewKafkaTransport(brokers []string) *KafkaTransport {
	return &KafkaTransport{
		brokers: append([]string(nil), brokers...),
		writers: make(map[string]*kafka.Writer),
	}
}

func (t *KafkaTransport) writerFor(topic string) *kafka.Writer {
	t.mu.Lock()
	defer t.mu.Unlock()
	if w, ok := t.writers[topic]; ok {
		return w
	}
	w := &kafka.Writer{
		Addr:                   kafka.TCP(t.brokers...),
		Topic:                  topic,
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireAll,
		BatchTimeout:           10 * time.Millisecond,
		AllowAutoTopicCreation: true,
	}
	t.writers[topic] = w
	return w
}

func (t *KafkaTransport) Publish(ctx context.Context, msg Message) error {
	w := t.writerFor(msg.Topic)
	return w.WriteMessages(ctx, kafka.Message{
		Key:   []byte(msg.Key),
		Value: msg.Payload,
		Headers: []kafka.Header{
			{Key: "event_id", Value: []byte(msg.EventID)},
		},
	})
}

func (t *KafkaTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var firstErr error
	for _, w := range t.writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// InMemoryTransport routes published messages directly to an in-process handler
// when KAFKA_BROKERS is unset (e.g. unit tests or single-node lightweight runs).
type InMemoryTransport struct {
	mu       sync.Mutex
	handlers map[string]func(context.Context, Message) error
	DLQ      []Message
}

func NewInMemoryTransport() *InMemoryTransport {
	return &InMemoryTransport{
		handlers: make(map[string]func(context.Context, Message) error),
	}
}

func (m *InMemoryTransport) Register(topic string, fn func(context.Context, Message) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[topic] = fn
}

func (m *InMemoryTransport) Publish(ctx context.Context, msg Message) error {
	m.mu.Lock()
	if len(msg.Topic) > 4 && msg.Topic[len(msg.Topic)-4:] == ".dlq" {
		m.DLQ = append(m.DLQ, msg)
		m.mu.Unlock()
		return nil
	}
	fn := m.handlers[msg.Topic]
	m.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, msg)
}

func (m *InMemoryTransport) Close() error { return nil }

// EnqueueTx inserts an outbox event inside the caller's MySQL transaction so domain state
// and message publication share an atomic commit boundary.
func EnqueueTx(ctx context.Context, tx *sql.Tx, eventID, topic, key string, payload any) error {
	if tx == nil {
		return errors.New("nil tx")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		"INSERT IGNORE INTO outbox_events(event_id,topic,partition_key,payload,status,attempts,next_attempt_at,created_at) VALUES(?,?,?,?,'pending',0,0,?)",
		eventID, topic, key, raw, time.Now().Unix(),
	)
	return err
}

// RelayStats provides hooks for Prometheus telemetry.
type RelayStats interface {
	RecordOutboxDispatched(n int64)
	RecordOutboxDLQ()
}

// OutboxRelay polls pending outbox_events rows and delivers them reliably with backoff + DLQ.
type OutboxRelay struct {
	db        *sql.DB
	transport Transport
	stats     RelayStats
	wakeCh    chan struct{}
	stopOnce  sync.Once
	stopCh    chan struct{}
	doneCh    chan struct{}
}

func NewOutboxRelay(db *sql.DB, transport Transport, stats RelayStats) *OutboxRelay {
	return &OutboxRelay{
		db:        db,
		transport: transport,
		stats:     stats,
		wakeCh:    make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

func (r *OutboxRelay) Start() {
	go r.loop()
}

func (r *OutboxRelay) Wake() {
	select {
	case r.wakeCh <- struct{}{}:
	default:
	}
}

func (r *OutboxRelay) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
		<-r.doneCh
	})
}

func (r *OutboxRelay) loop() {
	defer close(r.doneCh)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		r.FlushOnce(context.Background())
		select {
		case <-r.stopCh:
			return
		case <-r.wakeCh:
		case <-ticker.C:
		}
	}
}

type outboxRow struct {
	id       int64
	eventID  string
	topic    string
	key      string
	payload  []byte
	attempts int
}

// FlushOnce dispatches up to 32 ready outbox records.
func (r *OutboxRelay) FlushOnce(ctx context.Context) int {
	if r == nil || r.db == nil || r.transport == nil {
		return 0
	}
	now := time.Now().Unix()
	qCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	rows, err := r.db.QueryContext(qCtx,
		"SELECT id,event_id,topic,partition_key,payload,attempts FROM outbox_events WHERE status='pending' AND next_attempt_at<=? ORDER BY id ASC LIMIT 32",
		now,
	)
	cancel()
	if err != nil {
		return 0
	}
	var batch []outboxRow
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.id, &row.eventID, &row.topic, &row.key, &row.payload, &row.attempts); err != nil {
			continue
		}
		batch = append(batch, row)
	}
	rows.Close()

	dispatched := 0
	for _, row := range batch {
		pubCtx, pCancel := context.WithTimeout(ctx, 3*time.Second)
		err := r.transport.Publish(pubCtx, Message{
			EventID: row.eventID,
			Topic:   row.topic,
			Key:     row.key,
			Payload: row.payload,
		})
		pCancel()
		uCtx, uCancel := context.WithTimeout(ctx, 2*time.Second)
		if err == nil {
			_, _ = r.db.ExecContext(uCtx, "UPDATE outbox_events SET status='dispatched',dispatched_at=? WHERE id=?", time.Now().Unix(), row.id)
			dispatched++
			if r.stats != nil {
				r.stats.RecordOutboxDispatched(1)
			}
		} else {
			nextAttempt := row.attempts + 1
			errMsg := err.Error()
			if len(errMsg) > 500 {
				errMsg = errMsg[:500]
			}
			if nextAttempt >= maxRelayAttempts {
				_, _ = r.db.ExecContext(uCtx, "UPDATE outbox_events SET status='dlq',attempts=?,last_error=? WHERE id=?", nextAttempt, errMsg, row.id)
				dlqCtx, dCancel := context.WithTimeout(ctx, 2*time.Second)
				_ = r.transport.Publish(dlqCtx, Message{
					EventID: row.eventID,
					Topic:   row.topic + ".dlq",
					Key:     row.key,
					Payload: row.payload,
				})
				dCancel()
				if r.stats != nil {
					r.stats.RecordOutboxDLQ()
				}
				slog.Error("outbox_routed_to_dlq", "event_id", row.eventID, "topic", row.topic, "error", errMsg)
			} else {
				backoffSec := int64(1 << min(nextAttempt, 5))
				_, _ = r.db.ExecContext(uCtx, "UPDATE outbox_events SET attempts=?,next_attempt_at=?,last_error=? WHERE id=?", nextAttempt, time.Now().Unix()+backoffSec, errMsg, row.id)
			}
		}
		uCancel()
	}
	return dispatched
}
