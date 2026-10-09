// Package accountsql — the SQLite-backed account store behind a platform
// node. It is the durable counterpart of adapter/credstore: accounts seeded
// from YAML platform.accounts and mutated through the HTTP management API
// survive a process restart.
//
// One Store implements both domain ports: CredentialStore (the acceptor's
// Lookup, read-only) and AccountAdmin (the HTTP management plane). The
// schema is platform_accounts (node_id, username) → password, created_at.
//
// Passwords never reach a log line or an error message: every error names
// the node and the username, never the secret. The credentials constructed
// here carry a fixed realm — the platform-side digest verification uses the
// realm from the 401 challenge (the platform's serving realm), never from
// the stored credential.
package accountsql

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"

	_ "modernc.org/sqlite"
)

// storedRealm is the realm baked into the Credentials this store returns
// from Lookup. It exists only to satisfy NewCredentials' non-empty-realm
// rule; the acceptor verifies against the challenge realm, not this value.
const storedRealm = "sqlite-store"

// Store is the durable account store. The zero value is NOT usable;
// construct one with New over an initialised *sql.DB (the schema must have
// been bootstrapped by the storage package).
type Store struct {
	mu sync.RWMutex
	db *sql.DB
}

// New returns a store over db. The database must already carry the
// platform_accounts table (storage.Bootstrap runs the schema). The no_auth
// column is added on the spot for databases created before it existed —
// SQLite lacks ALTER TABLE IF NOT EXISTS, so "duplicate column" is the
// expected no-op signal, and any other failure aborts startup.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// NewE returns (store, error), running the no_auth column migration before
// handing the store back. Process startup uses this variant so a broken
// migration fails loudly instead of surfacing later as auth misfires.
func NewE(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if _, err := db.Exec(
		"ALTER TABLE platform_accounts ADD COLUMN no_auth INTEGER NOT NULL DEFAULT 0",
	); err != nil {
		// Driver error text is opaque: ask the schema whether the column
		// is already there. Yes → migration already applied; no → real
		// failure.
		var got string
		qerr := db.QueryRow(
			"SELECT name FROM pragma_table_info('platform_accounts') WHERE name='no_auth'",
		).Scan(&got)
		if qerr != nil {
			return nil, fmt.Errorf("accountsql: migrate no_auth column: %w", err)
		}
	}
	return s, nil
}

// Compile-time checks that the store satisfies both domain ports.
var (
	_ port.CredentialStore = (*Store)(nil)
	_ port.AccountAdmin    = (*Store)(nil)
)

// Lookup returns the credentials configured for username under nodeID. The
// second result is false when the platform has no such account, which the
// caller answers with 403 rather than a fresh challenge.
func (s *Store) Lookup(nodeID model.NodeID, username string) (model.Credentials, bool) {
	if username == "" {
		return model.Credentials{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var password string
	var noAuth int
	err := s.db.QueryRow(
		"SELECT password, no_auth FROM platform_accounts WHERE node_id=? AND username=?",
		nodeID.String(), username,
	).Scan(&password, &noAuth)
	if err != nil {
		// sql.ErrNoRows and any other read failure both mean "cannot
		// authenticate this username now", which the caller answers 403.
		// The error is deliberately swallowed: Lookup's contract is a
		// boolean, and leaking driver details to the wire adds nothing.
		return model.Credentials{}, false
	}
	cred, err := model.NewCredentials(username, storedRealm, password)
	if err != nil {
		return model.Credentials{}, false
	}
	if noAuth != 0 {
		cred = cred.WithNoAuth()
	}
	return cred, true
}

// ListAccounts returns the accounts of a node ordered by username. The view
// carries the username and the creation timestamp; never the password.
func (s *Store) ListAccounts(nodeID model.NodeID) ([]port.AccountInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(
		"SELECT username, created_at FROM platform_accounts WHERE node_id=? ORDER BY username",
		nodeID.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("accountsql: list accounts for node %s: %w", nodeID, err)
	}
	defer rows.Close()
	out := []port.AccountInfo{}
	for rows.Next() {
		var info port.AccountInfo
		if err := rows.Scan(&info.Username, &info.CreatedAt); err != nil {
			return nil, fmt.Errorf("accountsql: scan account for node %s: %w", nodeID, err)
		}
		out = append(out, info)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("accountsql: list accounts for node %s: %w", nodeID, err)
	}
	return out, nil
}

// AddAccount registers username/password under nodeID. An empty password
// is stored with no_auth=1 so Lookup reconstitutes the no-auth credential.
// A duplicate (node_id, username) is refused with port.ErrAccountExists
// (the caller maps it to 409).
func (s *Store) AddAccount(nodeID model.NodeID, username, password string) error {
	if username == "" {
		return fmt.Errorf("accountsql: add: empty username for node %s", nodeID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	noAuth := 0
	if password == "" {
		noAuth = 1
	}
	if _, err := s.db.Exec(
		"INSERT INTO platform_accounts (node_id, username, password, no_auth) VALUES (?, ?, ?, ?)",
		nodeID.String(), username, password, noAuth,
	); err != nil {
		// A unique-constraint failure from the driver is opaque; ask the
		// table whether the row is there now. The mutex serialises
		// writers, so a "yes" answer is the duplicate, not a race.
		var probe string
		perr := s.db.QueryRow(
			"SELECT password FROM platform_accounts WHERE node_id=? AND username=?",
			nodeID.String(), username,
		).Scan(&probe)
		if perr == nil {
			return fmt.Errorf("accountsql: add account %q for node %s: %w", username, nodeID, port.ErrAccountExists)
		}
		return fmt.Errorf("accountsql: add account %q for node %s: %w", username, nodeID, err)
	}
	return nil
}

// RemoveAccount drops the account for username under nodeID. An absent
// account is port.ErrAccountNotFound (the caller maps it to 404), not a
// silent no-op: a management delete that reports success for a name that
// was never there hides the mistake it should have surfaced.
func (s *Store) RemoveAccount(nodeID model.NodeID, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		"DELETE FROM platform_accounts WHERE node_id=? AND username=?",
		nodeID.String(), username,
	)
	if err != nil {
		return fmt.Errorf("accountsql: remove account %q from node %s: %w", username, nodeID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("accountsql: remove account %q from node %s: %w", username, nodeID, err)
	}
	if n == 0 {
		return fmt.Errorf("accountsql: account %q from node %s: %w", username, nodeID, port.ErrAccountNotFound)
	}
	return nil
}

// SetAccountPassword overwrites the stored password for username under
// nodeID. An absent account is port.ErrAccountNotFound.
func (s *Store) SetAccountPassword(nodeID model.NodeID, username, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		"UPDATE platform_accounts SET password=? WHERE node_id=? AND username=?",
		password, nodeID.String(), username,
	)
	if err != nil {
		return fmt.Errorf("accountsql: set password for account %q on node %s: %w", username, nodeID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("accountsql: set password for account %q on node %s: %w", username, nodeID, err)
	}
	if n == 0 {
		return fmt.Errorf("accountsql: account %q on node %s: %w", username, nodeID, port.ErrAccountNotFound)
	}
	return nil
}

// Add satisfies the scenario package's AccountStore interface so the store
// can be passed directly to RegisterNodeSteps. It delegates to AddAccount,
// mapping the duplicate to ErrAccountExists for the caller.
func (s *Store) Add(nodeID model.NodeID, cred model.Credentials) error {
	return s.AddAccount(nodeID, cred.Username(), cred.Password())
}

// Seed installs the credential (username, realm, password, no-auth flag)
// when absent and leaves an existing row untouched (INSERT OR IGNORE).
// Startup seeding is idempotent: YAML wins only on the first run, runtime
// changes made through AccountAdmin survive restarts. The realm is not
// stored — Lookup rebuilds credentials with the package-wide storedRealm
// because digest verification re-challenges with the live realm anyway.
func (s *Store) Seed(nodeID model.NodeID, cred model.Credentials) error {
	if cred.Username() == "" {
		return fmt.Errorf("accountsql: seed: empty username for node %s", nodeID)
	}
	noAuth := 0
	if cred.NoAuth() {
		noAuth = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(
		"INSERT OR IGNORE INTO platform_accounts (node_id, username, password, no_auth) VALUES (?, ?, ?, ?)",
		nodeID.String(), cred.Username(), cred.Password(), noAuth,
	); err != nil {
		return fmt.Errorf("accountsql: seed account %q for node %s: %w", cred.Username(), nodeID, err)
	}
	return nil
}
