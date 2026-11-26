package mq

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

type mqTestDriver struct{ state *mqTestState }
type mqTestConn struct{ state *mqTestState }
type mqTestRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}
type mqTestState struct {
	mu       sync.Mutex
	consumed map[string]bool
	status   map[int64]string
}

func (d *mqTestDriver) Open(string) (driver.Conn, error) { return &mqTestConn{state: d.state}, nil }
func (c *mqTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (c *mqTestConn) Close() error              { return nil }
func (c *mqTestConn) Begin() (driver.Tx, error) { return nil, errors.New("unsupported") }
func (c *mqTestConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if strings.Contains(q, "FROM mq_consumed_events") {
		key := args[0].Value.(string) + ":" + args[1].Value.(string)
		if c.state.consumed[key] {
			return &mqTestRows{cols: []string{"1"}, rows: [][]driver.Value{{int64(1)}}}, nil
		}
		return &mqTestRows{cols: []string{"1"}}, nil
	}
	if strings.Contains(q, "FROM outbox_events") {
		if c.state.status[1] == "" {
			c.state.status[1] = "pending"
			return &mqTestRows{
				cols: []string{"id", "event_id", "topic", "partition_key", "payload", "attempts"},
				rows: [][]driver.Value{{int64(1), "evt-1", TopicRewardBatch, "sky-island", []byte(`{"job_id":"j1"}`), int64(4)}},
			}, nil
		}
		return &mqTestRows{cols: []string{"id", "event_id", "topic", "partition_key", "payload", "attempts"}}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}
func (c *mqTestConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if strings.Contains(q, "INSERT IGNORE INTO mq_consumed_events") {
		key := args[0].Value.(string) + ":" + args[1].Value.(string)
		c.state.consumed[key] = true
		return driver.RowsAffected(1), nil
	}
	if strings.Contains(q, "UPDATE outbox_events SET status='dlq'") {
		c.state.status[1] = "dlq"
		return driver.RowsAffected(1), nil
	}
	if strings.Contains(q, "UPDATE outbox_events SET status='dispatched'") {
		c.state.status[1] = "dispatched"
		return driver.RowsAffected(1), nil
	}
	return driver.RowsAffected(1), nil
}
func (r *mqTestRows) Columns() []string { return r.cols }
func (r *mqTestRows) Close() error        { return nil }
func (r *mqTestRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

type failingTransport struct {
	dlq []Message
}

func (f *failingTransport) Publish(_ context.Context, msg Message) error {
	if strings.HasSuffix(msg.Topic, ".dlq") {
		f.dlq = append(f.dlq, msg)
		return nil
	}
	return errors.New("broker down")
}
func (f *failingTransport) Close() error { return nil }

func TestProcessIdempotent_DeduplicatesRetries(t *testing.T) {
	st := &mqTestState{consumed: map[string]bool{}, status: map[int64]string{}}
	db := sql.OpenDB(&mqConnector{state: st})
	defer db.Close()

	calls := 0
	ran1, err := ProcessIdempotent(context.Background(), db, "grp", "evt-dup", TopicRewardBatch, func() error {
		calls++
		return nil
	})
	if err != nil || !ran1 {
		t.Fatalf("first run failed: ran=%v err=%v", ran1, err)
	}
	ran2, err := ProcessIdempotent(context.Background(), db, "grp", "evt-dup", TopicRewardBatch, func() error {
		calls++
		return nil
	})
	if err != nil || ran2 || calls != 1 {
		t.Fatalf("expected second run skipped, got ran2=%v calls=%d err=%v", ran2, calls, err)
	}
}

func TestOutboxRelay_RoutesToDLQAfterMaxAttempts(t *testing.T) {
	st := &mqTestState{consumed: map[string]bool{}, status: map[int64]string{}}
	db := sql.OpenDB(&mqConnector{state: st})
	defer db.Close()

	tr := &failingTransport{}
	relay := NewOutboxRelay(db, tr, nil)
	relay.FlushOnce(context.Background())

	if st.status[1] != "dlq" || len(tr.dlq) != 1 || tr.dlq[0].EventID != "evt-1" {
		t.Fatalf("expected DLQ routing after attempt 5, got status=%q dlq=%+v", st.status[1], tr.dlq)
	}
}

type mqConnector struct{ state *mqTestState }

func (c *mqConnector) Connect(context.Context) (driver.Conn, error) {
	return &mqTestConn{state: c.state}, nil
}
func (c *mqConnector) Driver() driver.Driver { return &mqTestDriver{state: c.state} }
