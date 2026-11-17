// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/stdlib"
)

// TestPostgresFailover uses two dedicated PostgreSQL 17 Docker containers.
// .github/scripts/postgres-failover.sh owns their creation and cleanup.
func TestPostgresFailover(t *testing.T) {
	f := readFailoverFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	primaryState, err := f.inspect(ctx, f.primary, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.inspect(ctx, f.standby, true); err != nil {
		t.Fatal(err)
	}
	primary := openFailoverPG(t, f.dsn(f.primary.addr, "any"), nil)
	standby := openFailoverPG(t, f.dsn(f.standby.addr, "any"), nil)
	primaryIdentity := readFailoverIdentity(t, ctx, primary, false)
	standbyIdentity := readFailoverIdentity(t, ctx, standby, true)
	if primaryIdentity.systemID != standbyIdentity.systemID || primaryIdentity.node == standbyIdentity.node {
		t.Fatal("fixture must be two servers in the same physical replication cluster")
	}
	failoverPoll(t, ctx, "synchronous streaming standby", func(wait context.Context) (bool, error) {
		var ready bool
		err := primary.QueryRowContext(wait, `SELECT current_setting('synchronous_commit') = 'remote_apply'
			AND count(*) = 1 AND bool_and(state = 'streaming' AND sync_state IN ('sync', 'quorum'))
			FROM pg_stat_replication`).Scan(&ready)
		return ready, err
	})
	t.Logf("go-migrate failover fixture run %s: primary %s (version_num %d), standby %s (version_num %d)",
		f.run, f.primary.addr, primaryIdentity.version, f.standby.addr, standbyIdentity.version)

	dsn := f.dsn(f.primary.addr+","+f.standby.addr, "read-write")
	onPrimary := openFailoverPG(t, dsn, nil)
	if got := readFailoverIdentity(t, ctx, onPrimary, false); got != primaryIdentity {
		t.Fatal("multi-host DSN did not select the original primary")
	}

	// Seed each case through the production approval and execution path.
	cases := []*failoverCase{
		{schema: "failover_lost_ack", durable: 2, pending: []string{"3"}},
		{schema: "failover_before_commit", durable: 1, pending: []string{"2", "3"}},
	}
	for _, c := range cases {
		if _, err := primary.ExecContext(ctx, "CREATE SCHEMA "+c.schema); err != nil {
			t.Fatal(err)
		}
		m := c.migrator(t, onPrimary, nil)
		seedID := failoverPlan(t, ctx, m, "1", "1")
		result, err := m.UpTo(ctx, "1", ExpectTargetPlan(seedID))
		checkFailoverResult(t, result, err, "1")
		c.approved = failoverPlan(t, ctx, m, "", "2", "3")
	}

	lostAck := &failoverCommitFault{}
	result, err := cases[0].migrator(t, openFailoverPG(t, dsn, lostAck), lostAck).
		Up(ctx, ExpectTargetPlan(cases[0].approved))
	var migrationErr *MigrationError
	if !lostAck.confirmed.Load() || !errors.Is(err, ErrCommitOutcomeUnknown) ||
		!errors.As(err, &migrationErr) || migrationErr.Version != "2" || migrationErr.Direction != Up ||
		migrationErr.Statement != 0 || migrationErr.Dirty || !slices.Equal(ErrorCodes(err), []string{"commit_outcome_unknown"}) ||
		result == nil || len(result.Applied) != 0 {
		t.Fatalf("lost native COMMIT response: confirmation observed = %t, result = %+v, error = %v",
			lostAck.confirmed.Load(), result, err)
	}

	// Verify that the standby replayed the successful writes before the fence.
	var replayThrough string
	if err := primary.QueryRowContext(ctx, "SELECT pg_current_wal_flush_lsn()::text").Scan(&replayThrough); err != nil {
		t.Fatal(err)
	}
	failoverPoll(t, ctx, "standby WAL replay through "+replayThrough, func(wait context.Context) (bool, error) {
		var ready bool
		err := standby.QueryRowContext(wait, `SELECT pg_is_in_recovery()
			AND COALESCE(pg_last_wal_replay_lsn() >= $1::pg_lsn, false)`, replayThrough).Scan(&ready)
		return ready, err
	})
	for _, c := range cases {
		c.checkState(t, ctx, standby, c.durable)
	}
	t.Logf("standby replayed both seeds and the committed migration with the discarded response (WAL %s)", replayThrough)

	// The second COMMIT reaches the wire write only after its data and history
	// statements have completed on the pinned primary transaction. Stop the
	// container before letting pgx attempt to write it to the old socket.
	var fenced bool
	beforeCommit := &failoverCommitFault{beforeWrite: func() error {
		if err := f.fence(ctx, primaryState); err != nil {
			return err
		}
		fenced = true
		return nil
	}}
	result, err = cases[1].migrator(t, openFailoverPG(t, dsn, beforeCommit), beforeCommit).
		Up(ctx, ExpectTargetPlan(cases[1].approved))
	if !fenced || err == nil || errors.Is(err, ErrPlanChanged) || errors.Is(err, ErrDirty) ||
		result == nil || len(result.Applied) != 0 {
		t.Fatalf("fenced primary before COMMIT: fenced = %t, result = %+v, error = %v", fenced, result, err)
	}
	t.Log("stopped and verified the original primary before forwarding the second COMMIT; promoting the standby")

	promoteCtx, stopPromote := context.WithTimeout(ctx, 15*time.Second)
	defer stopPromote()
	var promoted bool
	if err := standby.QueryRowContext(promoteCtx, "SELECT pg_promote(true, 12)").Scan(&promoted); err != nil || !promoted {
		t.Fatalf("promote standby: completed = %t, error = %v", promoted, err)
	}
	// A fresh unwrapped pool uses the byte-identical DSN.
	onPromoted := openFailoverPG(t, dsn, nil)
	if got := readFailoverIdentity(t, ctx, onPromoted, false); got != standbyIdentity {
		t.Fatal("identical multi-host DSN did not select the promoted standby")
	}
	for _, c := range cases {
		t.Run(c.schema, func(t *testing.T) { c.checkRecovery(t, ctx, onPromoted) })
	}
}

type failoverCase struct {
	schema, approved string
	durable          int
	pending          []string
}

func (c *failoverCase) migrator(t *testing.T, db *sql.DB, fault *failoverCommitFault) *Migrator {
	t.Helper()
	table := c.schema + ".effects"
	opts := []Option{WithTable(c.schema + "." + DefaultTable), WithFS(files(
		"1_seed.sql", "CREATE TABLE "+table+" (n integer NOT NULL); INSERT INTO "+table+" VALUES (1);",
		"2_in_flight.sql", "INSERT INTO "+table+" VALUES (2);",
		"3_remaining.sql", "INSERT INTO "+table+" VALUES (3);",
	))}
	if fault != nil {
		opts = append(opts, WithHooks(Hooks{Before: func(_ context.Context, event Event) error {
			fault.armed.Store(event.Direction == Up && event.Migration.Version == "2")
			return nil
		}}))
	}
	m, err := New(db, "postgres", opts...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (c *failoverCase) checkRecovery(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	c.checkState(t, ctx, db, c.durable)
	m := c.migrator(t, db, nil)
	result, err := m.Up(ctx, ExpectTargetPlan(c.approved))
	if !errors.Is(err, ErrPlanChanged) || !slices.Equal(ErrorCodes(err), []string{"plan_changed"}) ||
		result == nil || len(result.Applied) != 0 {
		t.Fatalf("Up with stale approval: result = %+v, error = %v", result, err)
	}
	c.checkState(t, ctx, db, c.durable)
	freshID := failoverPlan(t, ctx, m, "", c.pending...)
	if freshID == c.approved {
		t.Fatal("approval ID did not change after promotion")
	}
	result, err = m.Up(ctx, ExpectTargetPlan(freshID))
	checkFailoverResult(t, result, err, c.pending...)
	result, err = m.Up(ctx, ExpectTargetPlan(freshID))
	if !errors.Is(err, ErrPlanChanged) || result == nil || len(result.Applied) != 0 {
		t.Fatalf("reuse of consumed approval: result = %+v, error = %v", result, err)
	}
	emptyID := failoverPlan(t, ctx, m, "")
	result, err = m.Up(ctx, ExpectTargetPlan(emptyID))
	checkFailoverResult(t, result, err)
	c.checkState(t, ctx, db, 3)
}

func (c *failoverCase) checkState(t *testing.T, ctx context.Context, db *sql.DB, applied int) {
	t.Helper()
	status, err := c.migrator(t, db, nil).Status(ctx)
	if err != nil || status == nil || status.Dirty || status.CallbackRecoveryRequired || status.Current != strconv.Itoa(applied) ||
		status.Pending != 3-applied || len(status.Migrations) != 3 {
		t.Fatalf("%s status = %+v, error = %v; want %d applied", c.schema, status, err, applied)
	}
	for i, m := range status.Migrations {
		want := StatePending
		if i < applied {
			want = StateApplied
		}
		if m.Version != strconv.Itoa(i+1) || m.State != want {
			t.Fatalf("%s migration %d: %+v; want %s", c.schema, i+1, m, want)
		}
	}

	history, data := make([]string, 0, applied), make([]string, 0, applied)
	for n := 1; n <= applied; n++ {
		history = append(history, fmt.Sprintf("%d:%d:true", n, n))
		data = append(data, strconv.Itoa(n))
	}
	var got string
	if err := db.QueryRowContext(ctx, `SELECT string_agg(format('%s:%s:%s', installed_rank, version, success::text),
		',' ORDER BY installed_rank) FROM `+c.schema+"."+DefaultTable).Scan(&got); err != nil || got != strings.Join(history, ",") {
		t.Fatalf("%s durable history = %q, error = %v; want %v", c.schema, got, err, history)
	}
	if err := db.QueryRowContext(ctx, "SELECT string_agg(n::text, ',' ORDER BY n) FROM "+c.schema+".effects").
		Scan(&got); err != nil || got != strings.Join(data, ",") {
		t.Fatalf("%s durable effects = %q, error = %v; want %v", c.schema, got, err, data)
	}
}

func failoverPlan(t *testing.T, ctx context.Context, m *Migrator, target string, want ...string) string {
	t.Helper()
	plan, id, err := m.PlanWithID(ctx, target)
	got := make([]string, 0, len(plan))
	for _, migration := range plan {
		got = append(got, migration.Version)
	}
	if err != nil || id == "" || !slices.Equal(got, want) {
		t.Fatalf("approved plan for %q = %v, error = %v; want %v", target, got, err, want)
	}
	return id
}

func checkFailoverResult(t *testing.T, result *Result, err error, want ...string) {
	t.Helper()
	if err != nil || result == nil || !slices.Equal(versions(result), want) {
		t.Fatalf("Up result = %+v, error = %v; want applied %v", result, err, want)
	}
}

// Keep the native pgx transaction and its network traffic. Interrupt only the
// selected simple-query COMMIT, either on its way out or on its way back.
type failoverCommitFault struct {
	armed, confirmed atomic.Bool
	beforeWrite      func() error
}

type failoverPGConn struct {
	net.Conn
	fault     *failoverCommitFault
	wantReply atomic.Bool
}

func (c *failoverPGConn) Write(b []byte) (int, error) {
	// pgx uses a simple Query ('Q') for COMMIT because it has no arguments.
	if c.fault.armed.Load() && bytes.Equal(b, []byte("Q\x00\x00\x00\x0bcommit\x00")) && c.fault.armed.Swap(false) {
		if c.fault.beforeWrite != nil {
			if err := c.fault.beforeWrite(); err != nil {
				return 0, err
			}
		} else {
			c.wantReply.Store(true)
		}
	}
	return c.Conn.Write(b)
}

func (c *failoverPGConn) Read(b []byte) (int, error) {
	if !c.wantReply.Swap(false) {
		return c.Conn.Read(b)
	}
	// Read the server's actual CommandComplete, then drop it and its
	// accompanying ReadyForQuery by closing the socket. pgx sees only EOF.
	msg, err := pgproto3.NewFrontend(c.Conn, io.Discard).Receive()
	_ = c.Close()
	if err != nil {
		return 0, err
	}
	tag, ok := msg.(*pgproto3.CommandComplete)
	if !ok || string(tag.CommandTag) != "COMMIT" {
		return 0, fmt.Errorf("expected PostgreSQL COMMIT CommandComplete, got %T", msg)
	}
	c.fault.confirmed.Store(true)
	return 0, io.EOF
}

func openFailoverPG(t *testing.T, dsn string, fault *failoverCommitFault) *sql.DB {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid PostgreSQL failover fixture DSN")
	}
	if fault != nil {
		cfg.AfterNetConnect = func(_ context.Context, _ *pgconn.Config, c net.Conn) (net.Conn, error) {
			return &failoverPGConn{Conn: c, fault: fault}, nil
		}
	}
	db := sql.OpenDB(stdlib.GetConnector(*cfg))
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type failoverIdentity struct {
	systemID, node string
	version        int
}

func readFailoverIdentity(t *testing.T, ctx context.Context, db *sql.DB, wantRecovery bool) failoverIdentity {
	t.Helper()
	var id failoverIdentity
	var recovery bool
	err := db.QueryRowContext(ctx, `SELECT system_identifier::text,
		inet_server_addr()::text || ':' || inet_server_port()::text,
		pg_is_in_recovery(), current_setting('server_version_num')::int
		FROM pg_control_system()`).Scan(&id.systemID, &id.node, &recovery, &id.version)
	if err != nil || id.systemID == "" || id.node == "" || recovery != wantRecovery || id.version/10000 != 17 {
		t.Fatalf("unexpected PostgreSQL fixture identity: recovery = %t (want %t), version = %d, error = %v",
			recovery, wantRecovery, id.version, err)
	}
	return id
}

func failoverPoll(t *testing.T, ctx context.Context, what string, ready func(context.Context) (bool, error)) {
	t.Helper()
	wait, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := ready(wait)
		if err == nil && ok {
			return
		}
		select {
		case <-wait.Done():
			t.Fatalf("waiting for %s: %v (last query error: %v)", what, wait.Err(), err)
		case <-ticker.C:
		}
	}
}

type failoverNode struct{ id, addr, role string }

type failoverFixture struct {
	run, password    string
	primary, standby failoverNode
}

func readFailoverFixture(t *testing.T) failoverFixture {
	t.Helper()
	const prefix = "GOMIGRATE_TEST_FAILOVER_"
	var optedIn bool
	get := func(key string) string {
		value, set := os.LookupEnv(prefix + key)
		optedIn = optedIn || set
		return value
	}
	f := failoverFixture{run: get("RUN"), password: get("PASSWORD"),
		primary: failoverNode{get("PRIMARY_ID"), get("PRIMARY_ADDR"), "primary"},
		standby: failoverNode{get("STANDBY_ID"), get("STANDBY_ADDR"), "standby"}}
	if !optedIn {
		t.Skip("requires dedicated PostgreSQL 17 fixture (.github/scripts/postgres-failover.sh)")
	}
	fullID := regexp.MustCompile(`^[a-f0-9]{64}$`)
	if f.run == "" || f.password == "" || !fullID.MatchString(f.primary.id) || !fullID.MatchString(f.standby.id) ||
		f.primary.id == f.standby.id || f.primary.addr == f.standby.addr {
		t.Fatal("incomplete or invalid dedicated PostgreSQL failover fixture")
	}
	for _, node := range []failoverNode{f.primary, f.standby} {
		host, port, err := net.SplitHostPort(node.addr)
		p, portErr := strconv.ParseUint(port, 10, 16)
		if err != nil || host != "127.0.0.1" || portErr != nil || p == 0 {
			t.Fatalf("fixture %s must have a distinct published loopback TCP port", node.role)
		}
	}
	return f
}

func (f failoverFixture) dsn(host, attrs string) string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword("postgres", f.password), Host: host, Path: "/postgres",
		RawQuery: "sslmode=disable&target_session_attrs=" + attrs + "&connect_timeout=3"}
	return u.String()
}

type failoverContainer struct {
	ID     string
	Config struct{ Labels map[string]string }
	State  struct {
		Running, Restarting bool
		StartedAt           string
	}
	HostConfig struct {
		RestartPolicy struct{ Name string }
	}
	NetworkSettings struct {
		Ports map[string][]struct{ HostIP, HostPort string }
	}
}

func (f failoverFixture) inspect(ctx context.Context, node failoverNode, running bool) (failoverContainer, error) {
	var zero failoverContainer
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "docker", "inspect", "--type", "container", node.id).Output()
	if err != nil {
		return zero, fmt.Errorf("inspect %s fixture container: %w", node.role, err)
	}
	var containers []failoverContainer
	if err := json.Unmarshal(out, &containers); err != nil || len(containers) != 1 {
		return zero, fmt.Errorf("unexpected Docker inspect record for %s fixture", node.role)
	}
	c := containers[0]
	l := c.Config.Labels
	if c.ID != node.id || l["project"] != "go-migrate" || l["owner"] != "codex-pg-failover" ||
		l["run"] != f.run || l["role"] != node.role || c.State.Running != running || c.State.Restarting ||
		c.State.StartedAt == "" || c.HostConfig.RestartPolicy.Name != "no" {
		return zero, fmt.Errorf("refusing %s fixture container without matching identity, ownership and running state", node.role)
	}
	if running {
		bindings := c.NetworkSettings.Ports["5432/tcp"]
		if len(bindings) != 1 || net.JoinHostPort(bindings[0].HostIP, bindings[0].HostPort) != node.addr {
			return zero, fmt.Errorf("fixture %s container does not own the specified loopback port", node.role)
		}
	}
	return c, nil
}

func (f failoverFixture) fence(ctx context.Context, initial failoverContainer) error {
	now, err := f.inspect(ctx, f.primary, true)
	if err != nil {
		return err
	}
	if now.State.StartedAt != initial.State.StartedAt {
		return errors.New("fixture primary container restarted before fencing")
	}
	stopCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(stopCtx, "docker", "stop", "--timeout", "10", f.primary.id).Run(); err != nil {
		return fmt.Errorf("fence owned primary container: %w", err)
	}
	stopped, err := f.inspect(ctx, f.primary, false)
	if err != nil {
		return err
	}
	if stopped.State.StartedAt != initial.State.StartedAt {
		return errors.New("fixture primary container changed during fencing")
	}
	return nil
}
