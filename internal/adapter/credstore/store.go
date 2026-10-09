// Package credstore — the in-memory account store behind a platform node.
//
// A platform authenticates its downstreams against accounts declared in the
// configuration. This adapter holds them, partitioned by node so two
// platforms can accept different passwords for the same device id.
//
// The store answers one question — "which secret goes with this username on
// this node?" — and cannot be enumerated, so "does this account exist" is
// never an oracle an attacker can probe.
package credstore

import (
	"fmt"
	"sort"
	"sync"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
)

// Compile-time check that the store also satisfies AccountAdmin.
var _ port.AccountAdmin = (*Store)(nil)

// Store is the process-wide account store. The zero value is NOT usable;
// construct one with New.
type Store struct {
	mu     sync.RWMutex
	byNode map[string]map[string]model.Credentials
}

// New returns an empty store.
func New() *Store {
	return &Store{byNode: make(map[string]map[string]model.Credentials)}
}

// Compile-time check that the store satisfies the domain port.
var (
	_ port.CredentialStore = (*Store)(nil)
	_ port.AccountSeeder   = (*Store)(nil)
)

// Seed installs the credential under nodeID when absent and leaves an
// existing entry untouched (idempotent startup seeding). The credential is
// stored as-is, preserving the realm and the no-auth flag the caller built.
func (s *Store) Seed(nodeID model.NodeID, cred model.Credentials) error {
	if cred.Username() == "" {
		return fmt.Errorf("credstore: seed: empty username for node %s", nodeID)
	}
	key := nodeID.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byNode[key] == nil {
		s.byNode[key] = make(map[string]model.Credentials)
	}
	if _, exists := s.byNode[key][cred.Username()]; exists {
		return nil
	}
	s.byNode[key][cred.Username()] = cred
	return nil
}

// Add registers cred under nodeID. A duplicate username for the same node
// is refused: two entries for one account would make "which password is
// right" depend on file order.
func (s *Store) Add(nodeID model.NodeID, cred model.Credentials) error {
	if cred.Username() == "" {
		return fmt.Errorf("credstore: add: empty username for node %s", nodeID)
	}
	key := nodeID.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byNode[key] == nil {
		s.byNode[key] = make(map[string]model.Credentials)
	}
	if _, exists := s.byNode[key][cred.Username()]; exists {
		// Deliberately does not say which password: the error is about
		// the username being declared twice.
		return fmt.Errorf("credstore: duplicate account %q for node %s", cred.Username(), nodeID)
	}
	s.byNode[key][cred.Username()] = cred
	return nil
}

// Lookup returns the credentials configured for username under nodeID. The
// second result is false when the platform has no such account, which the
// caller answers with 403 rather than a fresh challenge.
func (s *Store) Lookup(nodeID model.NodeID, username string) (model.Credentials, bool) {
	if username == "" {
		return model.Credentials{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	cred, ok := s.byNode[nodeID.String()][username]
	return cred, ok
}

// Remove drops the account for username under nodeID. Removing an absent
// account is a no-op.
func (s *Store) Remove(nodeID model.NodeID, username string) {
	key := nodeID.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts := s.byNode[key]
	if accounts == nil {
		return
	}
	delete(accounts, username)
	if len(accounts) == 0 {
		delete(s.byNode, key)
	}
}

// Clear drops every account of a node, so a stopped platform keeps no
// secrets in memory.
func (s *Store) Clear(nodeID model.NodeID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byNode, nodeID.String())
}

// --- port.AccountAdmin (string-node-ID flavour used by the management API) ---

// ListAccounts returns the non-sensitive view of every account under nodeID.
// The in-memory store does not track creation time, so CreatedAt stays empty;
// the sqlite-backed store fills it.
func (s *Store) ListAccounts(nodeID model.NodeID) ([]port.AccountInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	accounts := s.byNode[nodeID.String()]
	infos := make([]port.AccountInfo, 0, len(accounts))
	for username := range accounts {
		infos = append(infos, port.AccountInfo{Username: username})
	}
	// Stable order so management responses are reproducible in tests.
	sort.Slice(infos, func(i, j int) bool { return infos[i].Username < infos[j].Username })
	return infos, nil
}

// storedRealm is the realm baked into the Credentials this store returns
// from Lookup. It exists only to satisfy NewCredentials' non-empty-realm
// rule; the acceptor verifies against the challenge realm, not this value.
const storedRealm = "credstore-mem"

// AddAccount registers a new account under nodeID. It is
// idempotent: replacing the password of an existing username is accepted
// rather than rejected so callers need not read first.
func (s *Store) AddAccount(nodeID model.NodeID, username, password string) error {
	if username == "" {
		return fmt.Errorf("credstore: add: empty username")
	}
	if password == "" {
		return fmt.Errorf("credstore: add: empty password for %q", username)
	}
	cred, err := model.NewCredentials(username, storedRealm, password)
	if err != nil {
		return err
	}
	return s.Add(nodeID, cred)
}

// RemoveAccount drops the account for username under nodeID.
func (s *Store) RemoveAccount(nodeID model.NodeID, username string) error {
	s.Remove(nodeID, username)
	return nil
}

// SetAccountPassword rotates the password for username under nodeID; adding the
// account when it does not already exist.
func (s *Store) SetAccountPassword(nodeID model.NodeID, username, password string) error {
	if username == "" {
		return fmt.Errorf("credstore: set password: empty username")
	}
	if password == "" {
		return fmt.Errorf("credstore: set password: empty password for %q", username)
	}
	cred, err := model.NewCredentials(username, storedRealm, password)
	if err != nil {
		return err
	}
	// Replace unconditionally: remove then add so a stale entry never wins.
	s.Remove(nodeID, username)
	return s.Add(nodeID, cred)
}
