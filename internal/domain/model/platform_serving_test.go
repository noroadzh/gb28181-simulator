package model

import "testing"

func TestNewPlatformServing(t *testing.T) {
	policy, err := NewExpiresPolicy(30, 600, 1800)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	serving, err := NewPlatformServing("3402000000", policy)
	if err != nil {
		t.Fatalf("NewPlatformServing: %v", err)
	}
	if serving.Realm() != "3402000000" {
		t.Errorf("realm = %q, want 3402000000", serving.Realm())
	}
	if serving.Policy().Min() != 30 || serving.Policy().Default() != 600 || serving.Policy().Max() != 1800 {
		t.Errorf("policy = %d/%d/%d, want 30/600/1800",
			serving.Policy().Min(), serving.Policy().Default(), serving.Policy().Max())
	}
	if !serving.HasServing() {
		t.Error("HasServing() = false, want true")
	}
}

// A platform that declared nothing still serves: its home domain becomes
// the realm and the documented window applies.
func TestDefaultPlatformServing(t *testing.T) {
	serving, err := DefaultPlatformServing("3402000000")
	if err != nil {
		t.Fatalf("DefaultPlatformServing: %v", err)
	}
	if serving.Realm() != "3402000000" {
		t.Errorf("realm = %q, want the node's domain", serving.Realm())
	}
	if serving.Policy().Min() != DefaultPlatformMinExpires ||
		serving.Policy().Default() != DefaultPlatformExpires ||
		serving.Policy().Max() != DefaultPlatformMaxExpires {
		t.Errorf("policy = %d/%d/%d, want the defaults %d/%d/%d",
			serving.Policy().Min(), serving.Policy().Default(), serving.Policy().Max(),
			DefaultPlatformMinExpires, DefaultPlatformExpires, DefaultPlatformMaxExpires)
	}
}

func TestNewPlatformServing_EmptyRealmRejected(t *testing.T) {
	if _, err := NewPlatformServing("   ", ExpiresPolicy{}); err == nil {
		t.Fatal("NewPlatformServing with a blank realm: got nil, want an error")
	}
	if _, err := DefaultPlatformServing(""); err == nil {
		t.Fatal("DefaultPlatformServing with an empty domain: got nil, want an error")
	}
}

// A node that never declared a `platform:` section reports "not
// configured", which is the signal for the caller to fall back to the
// defaults instead of silently serving nobody.
func TestNodeProfile_PlatformServing(t *testing.T) {
	profile, err := NewNodeProfile("34020000002000000001", "127.0.0.1:15061", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	if _, ok := profile.PlatformServing(); ok {
		t.Fatal("PlatformServing() = true before it was set, want false")
	}
	serving, err := DefaultPlatformServing(profile.Domain())
	if err != nil {
		t.Fatalf("DefaultPlatformServing: %v", err)
	}
	with, err := profile.WithPlatformServing(serving)
	if err != nil {
		t.Fatalf("WithPlatformServing: %v", err)
	}
	got, ok := with.PlatformServing()
	if !ok {
		t.Fatal("PlatformServing() = false after it was set, want true")
	}
	if got.Realm() != "3402000000" {
		t.Errorf("realm = %q, want 3402000000", got.Realm())
	}
	if _, ok := profile.PlatformServing(); ok {
		t.Error("WithPlatformServing mutated the original profile")
	}
	if _, err := profile.WithPlatformServing(PlatformServing{}); err == nil {
		t.Error("WithPlatformServing with an empty serving: got nil, want an error")
	}
}

func TestNode_PlatformServingAccessor(t *testing.T) {
	profile, err := NewNodeProfile("34020000002000000001", "127.0.0.1:15061", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	serving, err := DefaultPlatformServing(profile.Domain())
	if err != nil {
		t.Fatalf("DefaultPlatformServing: %v", err)
	}
	profile, err = profile.WithPlatformServing(serving)
	if err != nil {
		t.Fatalf("WithPlatformServing: %v", err)
	}
	node := NewNode(profile)
	if _, ok := node.PlatformServing(); !ok {
		t.Error("Node.PlatformServing() = false, want true")
	}
}
