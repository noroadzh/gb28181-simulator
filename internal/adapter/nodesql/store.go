// Package nodesql is the SQLite-backed implementation of the runtime node
// configuration store. It is the only place in the simulator that reads
// the nodes, node_registrations and node_platforms tables: HTTP handlers
// and the NodeService talk to it through the port interface, so the SQL
// here can be replaced with anything else without leaking.
//
// The store is the system of record for node definitions. The NodeService
// in internal/app keeps an in-memory mirror so SIP goroutines never block
// on a database call, and this store is the authoritative source that
// refills that mirror after a process restart and that the admin HTTP API
// writes through to add, edit and delete nodes at runtime.
package nodesql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite" // registered as "sqlite" by init()

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// DriverName is the name the store registers with database/sql. The
// modernc.org/sqlite driver registers as "sqlite" via its own init().
const DriverName = "sqlite"

// Store is the SQL-backed implementation of port.NodeConfigurationStore.
// It is safe for concurrent use by any number of goroutines: every
// mutation runs in a single transaction so the three tables (nodes,
// node_registrations, node_platforms) always stay consistent.
type Store struct {
	db *sql.DB
}

// Compile-time assertion: Store satisfies the domain port. The interface
// is declared in internal/domain/port; this guarantees the adapter stays
// aligned with the contract the HTTP layer and NodeService depend on.
var _ port.NodeConfigurationStore = (*Store)(nil)

// Open opens (or creates) the SQLite database at dsn and returns a Store
// ready to use. dsn may be ":memory:" for tests, a file path, or any DSN
// the modernc driver understands. The store does not run migrations; the
// caller is expected to have applied them through storage.Bootstrap.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("nodesql: open %q: %w", dsn, err)
	}
	// modernc/sqlite is single-writer; let the caller decide whether to
	// share the connection pool or pin to one writer at a time.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("nodesql: ping %q: %w", dsn, err)
	}
	return &Store{db: db}, nil
}

// New returns a Store that uses the provided already-opened *sql.DB.
// The caller is responsible for closing the DB after the Store is no
// longer needed. This constructor is useful when the DB is created by
// storage.Bootstrap and should be shared rather than opened twice.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Close releases the underlying connection. After Close the Store is
// unusable.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the underlying *sql.DB for callers that need to bootstrap
// the schema. It is intended for tests and the migration helper; most
// callers should go through the Store methods.
func (s *Store) DB() *sql.DB { return s.db }

// ListNodes returns every node definition, ordered by id for stable
// output. Nodes without a registration or platform serving row are still
// returned; the corresponding HasRegistration / HasPlatform fields are
// false.
func (s *Store) ListNodes(ctx context.Context) ([]model.StoredNode, error) {
	rows, err := s.db.QueryContext(ctx, listNodesSQL)
	if err != nil {
		return nil, fmt.Errorf("nodesql: list nodes: %w", err)
	}
	defer rows.Close()
	out := make([]model.StoredNode, 0, 8)
	// Drain and close the nodes cursor before hydrating: Bootstrap caps
	// the pool at one connection, so a nested query (hydrate ->
	// loadRegistration) issued while rows still holds the only
	// connection would wait for itself forever.
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("nodesql: list nodes iter: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("nodesql: list nodes close: %w", err)
	}
	for i := range out {
		if err := s.hydrate(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GetNode returns one node or port.ErrNodeNotFound.
func (s *Store) GetNode(ctx context.Context, id model.NodeID) (model.StoredNode, error) {
	row := s.db.QueryRowContext(ctx, getNodeSQL, id.String())
	n, err := scanNode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.StoredNode{}, fmt.Errorf("%w: %s", port.ErrNodeNotFound, id.String())
		}
		return model.StoredNode{}, fmt.Errorf("nodesql: get %s: %w", id.String(), err)
	}
	if err := s.hydrate(ctx, &n); err != nil {
		return model.StoredNode{}, err
	}
	return n, nil
}

// CreateNode writes a new node plus its optional registration and
// platform rows in a single transaction. A duplicate id surfaces as
// port.ErrNodeAlreadyExists; the caller is expected to have validated the row first.
func (s *Store) CreateNode(ctx context.Context, in model.StoredNode) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, insertNodeSQL,
			in.ID.String(), in.Kind.String(), in.Domain, in.Addr, in.Vendor,
			in.CascadeParent, marshalChildren(in.CascadeChildren),
		); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: %s", port.ErrNodeAlreadyExists, in.ID.String())
			}
			return fmt.Errorf("nodesql: insert node %s: %w", in.ID.String(), err)
		}
		if in.HasRegistration {
			if err := insertRegistration(ctx, tx, in.ID, in.Registration); err != nil {
				return err
			}
		}
		if in.HasPlatform {
			if err := insertPlatform(ctx, tx, in.ID, in.Platform, in.CatalogPollIntervalMs); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateNode overwrites the node row plus its optional registration and
// platform rows. The transaction deletes the children first so a node
// that drops its registration does not keep a stale one around.
func (s *Store) UpdateNode(ctx context.Context, id model.NodeID, in model.StoredNode) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, updateNodeSQL,
			in.Kind.String(), in.Domain, in.Addr, in.Vendor,
			in.CascadeParent, marshalChildren(in.CascadeChildren),
			id.String(),
		)
		if err != nil {
			return fmt.Errorf("nodesql: update node %s: %w", id.String(), err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("nodesql: update node %s rows: %w", id.String(), err)
		}
		if n == 0 {
			return fmt.Errorf("%w: %s", port.ErrNodeNotFound, id.String())
		}
		// Replace child rows so a node that drops a registration does
		// not keep a stale one around.
		if _, err := tx.ExecContext(ctx, deleteRegistrationSQL, id.String()); err != nil {
			return fmt.Errorf("nodesql: clear registration for %s: %w", id.String(), err)
		}
		if _, err := tx.ExecContext(ctx, deletePlatformSQL, id.String()); err != nil {
			return fmt.Errorf("nodesql: clear platform for %s: %w", id.String(), err)
		}
		if in.HasRegistration {
			if err := insertRegistration(ctx, tx, id, in.Registration); err != nil {
				return err
			}
		}
		if in.HasPlatform {
			if err := insertPlatform(ctx, tx, id, in.Platform, in.CatalogPollIntervalMs); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteNode removes the node and, via the schema's ON DELETE CASCADE,
// its registration and platform rows. A missing row is reported as
// port.ErrNodeNotFound.
func (s *Store) DeleteNode(ctx context.Context, id model.NodeID) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, deleteNodeSQL, id.String())
		if err != nil {
			return fmt.Errorf("nodesql: delete node %s: %w", id.String(), err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("nodesql: delete node %s rows: %w", id.String(), err)
		}
		if n == 0 {
			return fmt.Errorf("%w: %s", port.ErrNodeNotFound, id.String())
		}
		return nil
	})
}

// UpdateCascade rewrites only the cascade fields. It is the cheap path
// used by the cascade-only PUT endpoint so a caller that only wants to
// edit routing does not have to resend the whole node row.
func (s *Store) UpdateCascade(ctx context.Context, id model.NodeID, parent string, children []string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, updateCascadeSQL,
			parent, marshalChildren(children), id.String(),
		)
		if err != nil {
			return fmt.Errorf("nodesql: update cascade %s: %w", id.String(), err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("nodesql: update cascade %s rows: %w", id.String(), err)
		}
		if n == 0 {
			return fmt.Errorf("%w: %s", port.ErrNodeNotFound, id.String())
		}
		return nil
	})
}

// UpsertRegistration writes the registration row for nodeID, replacing
// any existing one.
func (s *Store) UpsertRegistration(ctx context.Context, nodeID model.NodeID, reg model.Registration) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, deleteRegistrationSQL, nodeID.String()); err != nil {
			return fmt.Errorf("nodesql: clear registration for %s: %w", nodeID.String(), err)
		}
		return insertRegistration(ctx, tx, nodeID, reg)
	})
}

// UpsertPlatform writes the platform serving row for nodeID, replacing
// any existing one.
func (s *Store) UpsertPlatform(ctx context.Context, nodeID model.NodeID, plat model.PlatformServing, pollMs int) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, deletePlatformSQL, nodeID.String()); err != nil {
			return fmt.Errorf("nodesql: clear platform for %s: %w", nodeID.String(), err)
		}
		return insertPlatform(ctx, tx, nodeID, plat, pollMs)
	})
}

// --- internals ---

// withTx runs fn inside a single transaction, committing on success and
// rolling back on any error. The store opens the database with a single
// connection, so this serialises naturally; the explicit transaction
// keeps the three tables consistent when one of the writes fails.
func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("nodesql: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("nodesql: commit: %w", err)
	}
	return nil
}

// hydrate loads the optional registration and platform rows into n. A
// missing row is fine; the corresponding Has* flag stays false. The
// catalog poll interval only lives on the platform row.
func (s *Store) hydrate(ctx context.Context, n *model.StoredNode) error {
	reg, ok, err := s.loadRegistration(ctx, n.ID)
	if err != nil {
		return err
	}
	if ok {
		n.Registration = reg
		n.HasRegistration = true
	}
	plat, pollMs, ok, err := s.loadPlatform(ctx, n.ID)
	if err != nil {
		return err
	}
	if ok {
		n.Platform = plat
		n.HasPlatform = true
		n.CatalogPollIntervalMs = pollMs
	}
	return nil
}

// loadRegistration fetches the registration row. The (server, server_id)
// pair is the redundant (address, id) the schema stores side by side.
func (s *Store) loadRegistration(ctx context.Context, id model.NodeID) (model.Registration, bool, error) {
	row := s.db.QueryRowContext(ctx, selectRegistrationSQL, id.String())
	var (
		server, serverID, username, password, gbVer, transport sql.NullString
		expires, timeoutMs, hbIntervalMs, hbTimeoutMs          sql.NullInt64
		hbMaxFailures, allowNoAuth                             sql.NullInt64
	)
	if err := row.Scan(&server, &serverID, &username, &password, &gbVer, &expires,
		&timeoutMs, &transport, &allowNoAuth,
		&hbIntervalMs, &hbTimeoutMs, &hbMaxFailures); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Registration{}, false, nil
		}
		return model.Registration{}, false, fmt.Errorf("nodesql: load registration %s: %w", id.String(), err)
	}
	params := model.RegistrationParams{
		Server:               server.String,
		ServerID:             serverID.String,
		Username:             username.String,
		Password:             password.String,
		GBVersion:            gbVer.String,
		Expires:              uint32(expires.Int64),
		Timeout:              msToDuration(timeoutMs.Int64),
		Transport:            strings.ToLower(transport.String),
		AllowNoAuth:          allowNoAuth.Int64 != 0,
		HeartbeatInterval:    msToDuration(hbIntervalMs.Int64),
		HeartbeatTimeout:     msToDuration(hbTimeoutMs.Int64),
		HeartbeatMaxFailures: uint32(hbMaxFailures.Int64),
	}
	reg, err := model.NewRegistration(params)
	if err != nil {
		return model.Registration{}, false, fmt.Errorf("nodesql: rebuild registration %s: %w", id.String(), err)
	}
	return reg, true, nil
}

// loadPlatform fetches the platform serving row.
func (s *Store) loadPlatform(ctx context.Context, id model.NodeID) (model.PlatformServing, int, bool, error) {
	row := s.db.QueryRowContext(ctx, selectPlatformSQL, id.String())
	var (
		realm   string
		allowNo int64
		minExp  int64
		defExp  int64
		maxExp  int64
		pollMs  int64
	)
	if err := row.Scan(&realm, &allowNo, &minExp, &defExp, &maxExp, &pollMs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.PlatformServing{}, 0, false, nil
		}
		return model.PlatformServing{}, 0, false, fmt.Errorf("nodesql: load platform %s: %w", id.String(), err)
	}
	policy, err := model.NewExpiresPolicy(uint32(minExp), uint32(defExp), uint32(maxExp))
	if err != nil {
		return model.PlatformServing{}, 0, false, fmt.Errorf("nodesql: rebuild platform policy %s: %w", id.String(), err)
	}
	plat, err := model.NewPlatformServingWithNoAuth(realm, policy, allowNo != 0)
	if err != nil {
		return model.PlatformServing{}, 0, false, fmt.Errorf("nodesql: rebuild platform %s: %w", id.String(), err)
	}
	return plat, int(pollMs), true, nil
}

// insertRegistration writes the registration row for id. AllowNoAuth
// is reconstructed from the model — NewRegistration keeps it on the
// value so the adapter can round-trip it without a parallel table.
func insertRegistration(ctx context.Context, tx *sql.Tx, id model.NodeID, reg model.Registration) error {
	_, err := tx.ExecContext(ctx, insertRegistrationSQL,
		id.String(), reg.Server(), reg.ServerID(), reg.Username(), reg.Password(),
		reg.GBVersion(), reg.Expires(), durationToMs(reg.Timeout()),
		strings.ToUpper(reg.Transport()),
		boolToInt(reg.AllowNoAuth()),
		durationToMs(reg.HeartbeatInterval()),
		durationToMs(reg.HeartbeatTimeout()),
		reg.MaxHeartbeatFailures(),
	)
	if err != nil {
		return fmt.Errorf("nodesql: insert registration %s: %w", id.String(), err)
	}
	return nil
}

// insertPlatform writes the platform serving row for id. The allow_no_auth
// policy bit is round-tripped from the model — it is a server-side policy,
// not a secret, so it persists alongside the realm and lifetime window.
func insertPlatform(ctx context.Context, tx *sql.Tx, id model.NodeID, plat model.PlatformServing, pollMs int) error {
	policy := plat.Policy()
	_, err := tx.ExecContext(ctx, insertPlatformSQL,
		id.String(), plat.Realm(), boolToInt(plat.AllowNoAuth()),
		policy.Min(), policy.Default(), policy.Max(), pollMs,
	)
	if err != nil {
		return fmt.Errorf("nodesql: insert platform %s: %w", id.String(), err)
	}
	return nil
}

// scanNode reads a single row from a QueryRow or Rows iterator.
func scanNode(s scanner) (model.StoredNode, error) {
	var (
		rawID, kind, domain, addr, vendor string
		parent                            string
		childrenJSON                      string
		createdAt, updatedAt              string
	)
	if err := s.Scan(&rawID, &kind, &domain, &addr, &vendor, &parent, &childrenJSON, &createdAt, &updatedAt); err != nil {
		return model.StoredNode{}, err
	}
	id, err := model.ParseNodeID(rawID)
	if err != nil {
		return model.StoredNode{}, fmt.Errorf("nodesql: row %q has bad id: %w", rawID, err)
	}
	nk, err := model.ParseNodeKind(kind)
	if err != nil {
		return model.StoredNode{}, fmt.Errorf("nodesql: row %s has bad kind %q: %w", rawID, kind, err)
	}
	children, err := unmarshalChildren(childrenJSON)
	if err != nil {
		return model.StoredNode{}, fmt.Errorf("nodesql: row %s has bad cascade_children: %w", rawID, err)
	}
	stampCreated, err := parseTimestamp(createdAt)
	if err != nil {
		return model.StoredNode{}, fmt.Errorf("nodesql: row %s has bad created_at: %w", rawID, err)
	}
	stampUpdated, err := parseTimestamp(updatedAt)
	if err != nil {
		return model.StoredNode{}, fmt.Errorf("nodesql: row %s has bad updated_at: %w", rawID, err)
	}
	return model.StoredNode{
		ID:              id,
		Kind:            nk,
		Domain:          domain,
		Addr:            addr,
		Vendor:          vendor,
		CascadeParent:   parent,
		CascadeChildren: children,
		CreatedAt:       stampCreated,
		UpdatedAt:       stampUpdated,
	}, nil
}

// parseTimestamp converts the `datetime('now')` text SQLite writes —
// "2006-01-02 15:04:05", read as UTC — into a time.Time. An empty cell
// (possible on rows written by the YAML import path before it stamped
// timestamps) reads as the zero time rather than an error.
func parseTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02 15:04:05", s)
}

// scanner is the intersection of *sql.Row and *sql.Rows: anything with a
// Scan method takes the row shape the store uses.
type scanner interface {
	Scan(dest ...any) error
}

// marshalChildren turns the children slice into the JSON array the schema
// stores. A nil slice is stored as [] so the column is well-formed.
func marshalChildren(in []string) string {
	if in == nil {
		return "[]"
	}
	b, err := json.Marshal(in)
	if err != nil {
		// json.Marshal of []string cannot fail at runtime, but the
		// caller-facing error is clearer than a panic.
		return "[]"
	}
	return string(b)
}

// unmarshalChildren reverses marshalChildren. An empty or missing column
// yields a nil slice.
func unmarshalChildren(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// isUniqueViolation recognises the SQLite constraint codes that mean
// "this row already exists": modernc.org/sqlite reports the *extended*
// result code from Error.Code(), not the primary code 19. A duplicate
// PRIMARY KEY (nodes.id) surfaces as SQLITE_CONSTRAINT_PRIMARYKEY and a
// duplicate on any other UNIQUE column as SQLITE_CONSTRAINT_UNIQUE; both
// are the same situation for the caller and both map to
// port.ErrNodeAlreadyExists. NOT NULL / CHECK / FOREIGN KEY violations
// are real data errors and must not be relabelled, so they are excluded.
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	const (
		sqliteConstraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
		sqliteConstraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
	)
	return se.Code() == sqliteConstraintPrimaryKey || se.Code() == sqliteConstraintUnique
}

// durationToMs / msToDuration convert a time.Duration to/from the integer
// millisecond columns the schema stores. Sub-millisecond values are
// rounded down to the nearest millisecond.
func durationToMs(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return d.Milliseconds()
}

func msToDuration(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

// boolToInt is a small helper to keep SQL writes tidy.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SQL statements. They are file-scope so the driver can prepare them
// once and we can keep the table layout here, in one place. They mirror
// the schema in internal/storage/migrations/001_runtime_node_storage.sql
// one-to-one, so a column rename there is a column rename here.
const (
	// listNodesSQL reads every node definition in id order. The
	// cascade_children column is the raw JSON. timestamps are read
	// so the HTTP layer can surface them.
	listNodesSQL = `SELECT id, kind, domain, addr, vendor, cascade_parent, cascade_children,
	                created_at, updated_at
	                FROM nodes ORDER BY id`
	// getNodeSQL is listNodesSQL restricted to one id.
	getNodeSQL = `SELECT id, kind, domain, addr, vendor, cascade_parent, cascade_children,
	              created_at, updated_at
	              FROM nodes WHERE id = ?`
	// insertNodeSQL writes a fresh node row. cascade_parent and
	// cascade_children default on the column itself, but the explicit
	// values keep the round-trip predictable.
	insertNodeSQL = `INSERT INTO nodes
	    (id, kind, domain, addr, vendor, cascade_parent, cascade_children)
	    VALUES (?, ?, ?, ?, ?, ?, ?)`
	// updateNodeSQL rewrites everything except the id and the
	// timestamps. ON DELETE CASCADE is what removes the children
	// (registration, platform) when a node is deleted, but on update
	// we replace them explicitly.
	updateNodeSQL = `UPDATE nodes SET
	    kind = ?, domain = ?, addr = ?, vendor = ?,
	    cascade_parent = ?, cascade_children = ?,
	    updated_at = datetime('now')
	    WHERE id = ?`
	// updateCascadeSQL only rewrites the two cascade columns. It is
	// the cheap path for the cascade-only PUT.
	updateCascadeSQL = `UPDATE nodes SET
	    cascade_parent = ?, cascade_children = ?,
	    updated_at = datetime('now')
	    WHERE id = ?`
	// deleteNodeSQL removes the row; the FK ON DELETE CASCADE
	// handles the children.
	deleteNodeSQL = `DELETE FROM nodes WHERE id = ?`

	// deleteRegistrationSQL / deletePlatformSQL clear a child row
	// before a fresh insert so update paths do not have to know
	// whether the row exists.
	deleteRegistrationSQL = `DELETE FROM node_registrations WHERE node_id = ?`
	deletePlatformSQL     = `DELETE FROM node_platforms WHERE node_id = ?`

	// insertRegistrationSQL writes one row. server and server_id are
	// the (address, id) pair the schema stores side by side. The
	// catalog poll interval lives on node_platforms, not here.
	insertRegistrationSQL = `INSERT INTO node_registrations
	    (node_id, server, server_id, username, password, gb_version,
	     expires, timeout_ms, transport, allow_no_auth,
	     heartbeat_interval_ms, heartbeat_timeout_ms, heartbeat_max_failures)
	    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	// insertPlatformSQL writes the platform serving row.
	insertPlatformSQL = `INSERT INTO node_platforms
	    (node_id, realm, allow_no_auth, min_expires, default_expires,
	     max_expires, catalog_poll_interval_ms)
	    VALUES (?, ?, ?, ?, ?, ?, ?)`

	// selectRegistrationSQL is paired with loadRegistration.
	selectRegistrationSQL = `SELECT server, server_id, username, password, gb_version, expires,
	    timeout_ms, transport, allow_no_auth,
	    heartbeat_interval_ms, heartbeat_timeout_ms, heartbeat_max_failures
	    FROM node_registrations WHERE node_id = ?`
	// selectPlatformSQL is paired with loadPlatform.
	selectPlatformSQL = `SELECT realm, allow_no_auth, min_expires, default_expires, max_expires,
	    catalog_poll_interval_ms
	    FROM node_platforms WHERE node_id = ?`
)
