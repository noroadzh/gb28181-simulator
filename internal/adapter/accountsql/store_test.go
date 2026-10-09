// Package accountsql — tests for the durable account store.
package accountsql

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	"github.com/your-org/gb28181-simulator/internal/domain/port"
	"github.com/your-org/gb28181-simulator/internal/storage"
)

// newTestStore opens a throwaway sqlite database (file-backed temp dir so
// the on-disk schema bootstrap runs exactly as in production) and returns a
// ready Store.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Bootstrap(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

func mustNodeID(t *testing.T, id string) model.NodeID {
	t.Helper()
	nid, err := model.ParseNodeID(id)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", id, err)
	}
	return nid
}

func TestSeedIdempotent(t *testing.T) {
	s := newTestStore(t)
	node := mustNodeID(t, "34020000002000000001")

	cred1, err := model.NewCredentials("alice", "test-realm", "pw-1")
	if err != nil {
		t.Fatalf("new credentials 1: %v", err)
	}
	if err := s.Seed(node, cred1); err != nil {
		t.Fatalf("seed 1: %v", err)
	}
	cred2, err := model.NewCredentials("alice", "test-realm", "pw-CHANGED")
	if err != nil {
		t.Fatalf("new credentials 2: %v", err)
	}
	if err := s.Seed(node, cred2); err != nil {
		t.Fatalf("seed 2: %v", err)
	}
	// The second seed must not have overwritten the first.
	cred, ok := s.Lookup(node, "alice")
	if !ok {
		t.Fatal("lookup after seed: account missing")
	}
	if !cred.PasswordEquals("pw-1") {
		t.Error("seed overwrote an existing password; INSERT OR IGNORE must not")
	}
}

func TestCRUD(t *testing.T) {
	s := newTestStore(t)
	node := mustNodeID(t, "34020000002000000001")

	// Add.
	if err := s.AddAccount(node, "bob", "pw-b"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.AddAccount(node, "alice", "pw-a"); err != nil {
		t.Fatalf("add: %v", err)
	}

	// List is ordered by username and carries no password.
	accounts, err := s.ListAccounts(node)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("list: got %d accounts, want 2", len(accounts))
	}
	if accounts[0].Username != "alice" || accounts[1].Username != "bob" {
		t.Errorf("list order: %q, %q (want alice, bob)", accounts[0].Username, accounts[1].Username)
	}
	if accounts[0].CreatedAt == "" {
		t.Error("list: created_at is empty")
	}

	// Lookup hit.
	cred, ok := s.Lookup(node, "bob")
	if !ok {
		t.Fatal("lookup bob: not found")
	}
	if cred.Username() != "bob" || !cred.PasswordEquals("pw-b") {
		t.Errorf("lookup bob: got username=%q", cred.Username())
	}

	// Lookup miss.
	if _, ok := s.Lookup(node, "nobody"); ok {
		t.Error("lookup nobody: unexpected hit")
	}
	if _, ok := s.Lookup(node, ""); ok {
		t.Error("lookup empty username: unexpected hit")
	}

	// SetAccountPassword changes the secret and nothing else.
	if err := s.SetAccountPassword(node, "bob", "pw-b2"); err != nil {
		t.Fatalf("set password: %v", err)
	}
	cred, _ = s.Lookup(node, "bob")
	if !cred.PasswordEquals("pw-b2") {
		t.Error("set password: lookup still answers the old secret")
	}

	// Remove drops the row; a second remove is 404.
	if err := s.RemoveAccount(node, "bob"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := s.Lookup(node, "bob"); ok {
		t.Error("lookup bob after remove: still there")
	}
	if err := s.RemoveAccount(node, "bob"); !errors.Is(err, port.ErrAccountNotFound) {
		t.Errorf("remove absent: err = %v, want ErrAccountNotFound", err)
	}
}

func TestAddDuplicateIsErrAccountExists(t *testing.T) {
	s := newTestStore(t)
	node := mustNodeID(t, "34020000002000000001")

	if err := s.AddAccount(node, "carol", "pw-1"); err != nil {
		t.Fatalf("add 1: %v", err)
	}
	err := s.AddAccount(node, "carol", "pw-2")
	if !errors.Is(err, port.ErrAccountExists) {
		t.Fatalf("add duplicate: err = %v, want ErrAccountExists", err)
	}
	// The original secret must survive the failed add.
	cred, _ := s.Lookup(node, "carol")
	if !cred.PasswordEquals("pw-1") {
		t.Error("failed add overwrote the stored password")
	}
}

func TestSetPasswordAbsentIsNotFound(t *testing.T) {
	s := newTestStore(t)
	node := mustNodeID(t, "34020000002000000001")

	err := s.SetAccountPassword(node, "ghost", "pw")
	if !errors.Is(err, port.ErrAccountNotFound) {
		t.Errorf("set password on absent account: err = %v, want ErrAccountNotFound", err)
	}
}

func TestAccountsArePartitionedByNode(t *testing.T) {
	s := newTestStore(t)
	nodeA := mustNodeID(t, "34020000002000000001")
	nodeB := mustNodeID(t, "34020000002000000002")

	var credA, credB model.Credentials
	var err error
	credA, err = model.NewCredentials("shared", "test-realm", "pw-a")
	if err != nil {
		t.Fatalf("new credentials a: %v", err)
	}
	if err := s.Seed(nodeA, credA); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	credB, err = model.NewCredentials("shared", "test-realm", "pw-b")
	if err != nil {
		t.Fatalf("new credentials b: %v", err)
	}
	if err := s.Seed(nodeB, credB); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	gotA, _ := s.Lookup(nodeA, "shared")
	gotB, _ := s.Lookup(nodeB, "shared")
	if !gotA.PasswordEquals("pw-a") || !gotB.PasswordEquals("pw-b") {
		t.Error("accounts leaked across nodes")
	}
	accounts, err := s.ListAccounts(nodeA)
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	if len(accounts) != 1 {
		t.Errorf("list a: got %d accounts, want 1", len(accounts))
	}
}

// TestConcurrentAddSameNodeDifferentUsernames exercises the mutex under -race.
func TestConcurrentAddSameNodeDifferentUsernames(t *testing.T) {
	s := newTestStore(t)
	node := mustNodeID(t, "34020000002000000001")

	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.AddAccount(node, concurrentUsername(i), "pw")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	accounts, err := s.ListAccounts(node)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(accounts) != workers {
		t.Errorf("list: got %d accounts, want %d", len(accounts), workers)
	}
	_ = context.Background // keep the context import if unused later
}

func concurrentUsername(i int) string {
	return "user-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
}

// TestErrorsNeverCarryThePassword asserts the password never appears in an
// error message — the store's documented contract.
func TestErrorsNeverCarryThePassword(t *testing.T) {
	s := newTestStore(t)
	node := mustNodeID(t, "34020000002000000001")

	const secret = "s3cret-DONT-LEAK"
	if err := s.AddAccount(node, "dup", secret); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for name, err := range map[string]error{
		"duplicate add":       s.AddAccount(node, "dup", secret),
		"remove absent":       s.RemoveAccount(node, "ghost"),
		"set password absent": s.SetAccountPassword(node, "ghost", secret),
		// AddAccount rejects empty username at store level; Seed never
		// receives an empty username because the caller (main.go) validates
		// through model.NewCredentials first.
		"add empty username": s.AddAccount(node, "", secret),
	} {
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}
		if contains(err.Error(), secret) {
			t.Errorf("%s: error message carries the password: %q", name, err.Error())
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && stringsIndex(haystack, needle) >= 0
}

func stringsIndex(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
