package credstore

import (
	"sync"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func mustNodeID(t *testing.T, id string) model.NodeID {
	t.Helper()
	nodeID, err := model.ParseNodeID(id)
	if err != nil {
		t.Fatalf("ParseNodeID(%q): %v", id, err)
	}
	return nodeID
}

func mustCreds(t *testing.T, username, realm, password string) model.Credentials {
	t.Helper()
	cred, err := model.NewCredentials(username, realm, password)
	if err != nil {
		t.Fatalf("NewCredentials(%q): %v", username, err)
	}
	return cred
}

func TestStore_Lookup(t *testing.T) {
	s := New()
	nodeID := mustNodeID(t, "34020000002000000001")
	if err := s.Add(nodeID, mustCreds(t, "34020000011310000001", "3402000000", "secret")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	cred, ok := s.Lookup(nodeID, "34020000011310000001")
	if !ok {
		t.Fatal("Lookup = false, want the configured account")
	}
	if !cred.PasswordEquals("secret") {
		t.Error("password does not match the configured one")
	}
	if cred.PasswordEquals("wrong") {
		t.Error("PasswordEquals accepted the wrong password")
	}
	if _, ok := s.Lookup(nodeID, "34020000041310000099"); ok {
		t.Error("Lookup found an account that was never added")
	}
	if _, ok := s.Lookup(nodeID, ""); ok {
		t.Error("Lookup with an empty username: got true, want false")
	}
}

// Two platforms can accept different passwords for the same device id:
// accounts are partitioned by node, not global.
func TestStore_PartitionedByNode(t *testing.T) {
	s := New()
	first := mustNodeID(t, "34020000002000000001")
	second := mustNodeID(t, "34020000002000000002")
	if err := s.Add(first, mustCreds(t, "34020000011310000001", "3402000000", "one")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(second, mustCreds(t, "34020000011310000001", "3402000000", "two")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	cred, ok := s.Lookup(second, "34020000011310000001")
	if !ok || !cred.PasswordEquals("two") {
		t.Fatalf("node 2's password = %v (found=%v), want \"two\"", ok, ok)
	}
	cred, ok = s.Lookup(first, "34020000011310000001")
	if !ok || !cred.PasswordEquals("one") {
		t.Fatal("node 1 lost its own password")
	}
}

func TestStore_RejectsDuplicateAndEmpty(t *testing.T) {
	s := New()
	nodeID := mustNodeID(t, "34020000002000000001")
	if err := s.Add(nodeID, mustCreds(t, "34020000011310000001", "3402000000", "secret")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	err := s.Add(nodeID, mustCreds(t, "34020000011310000001", "3402000000", "other"))
	if err == nil {
		t.Fatal("Add with a duplicate username: got nil, want an error")
	}
	// The error is about the username being declared twice; it must not
	// carry either password.
	if contains := err.Error(); contains == "" {
		t.Fatal("error is empty")
	}
	if err := s.Add(nodeID, model.Credentials{}); err == nil {
		t.Error("Add with an empty username: got nil, want an error")
	}
}

func TestStore_RemoveAndClear(t *testing.T) {
	s := New()
	nodeID := mustNodeID(t, "34020000002000000001")
	if err := s.Add(nodeID, mustCreds(t, "34020000011310000001", "3402000000", "secret")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.Remove(nodeID, "34020000041310000099") // absent: no-op
	if _, ok := s.Lookup(nodeID, "34020000011310000001"); !ok {
		t.Fatal("removing an absent account removed a real one")
	}
	s.Remove(nodeID, "34020000011310000001")
	if _, ok := s.Lookup(nodeID, "34020000011310000001"); ok {
		t.Error("account still present after Remove")
	}
	if err := s.Add(nodeID, mustCreds(t, "34020000011310000001", "3402000000", "secret")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.Clear(nodeID)
	if _, ok := s.Lookup(nodeID, "34020000011310000001"); ok {
		t.Error("account still present after Clear")
	}
}

// The serving goroutine looks accounts up while configuration reloads or
// tests mutate them, so the store must be safe under -race.
func TestStore_ConcurrentUse(t *testing.T) {
	s := New()
	nodeID := mustNodeID(t, "34020000002000000001")
	if err := s.Add(nodeID, mustCreds(t, "34020000011310000001", "3402000000", "secret")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cred, ok := s.Lookup(nodeID, "34020000011310000001"); ok {
				_ = cred.PasswordEquals("secret")
			}
		}()
	}
	wg.Wait()
}
