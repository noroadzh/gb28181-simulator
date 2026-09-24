package main

import (
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/credstore"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
)

// The step where a configured password becomes a credential is the one place
// a typing mistake would silently lock every device out, so it is wired the
// same way in a test as it is at start-up.
func TestApplyPlatformSection(t *testing.T) {
	profile, err := model.NewNodeProfile("34020000002000000001", "127.0.0.1:15061", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	realm := "3402000001"
	accounts := credstore.New()
	got, err := applyPlatformSection(profile, &platformconfig.NodePlatformConfig{
		Realm:   &realm,
		Min:     60,
		Default: 1800,
		Max:     7200,
		Accounts: []platformconfig.NodePlatformAccount{
			{Username: "34020000011310000001", Password: "secret"},
		},
	}, accounts)
	if err != nil {
		t.Fatalf("applyPlatformSection: %v", err)
	}
	serving, ok := got.PlatformServing()
	if !ok {
		t.Fatal("the profile carries no serving description")
	}
	if serving.Realm() != realm {
		t.Errorf("realm = %q, want the configured %q", serving.Realm(), realm)
	}
	if serving.Policy().Default() != 1800 || serving.Policy().Max() != 7200 {
		t.Errorf("window = %d/%d, want 1800 as the default", serving.Policy().Default(), serving.Policy().Max())
	}
	cred, ok := accounts.Lookup(got.ID(), "34020000011310000001")
	if !ok {
		t.Fatal("the account was not handed to the store")
	}
	if !cred.PasswordEquals("secret") {
		t.Error("the stored password is not the configured one")
	}
	if cred.Realm() != realm {
		t.Errorf("credential realm = %q, want %q", cred.Realm(), realm)
	}
}

// A node whose `platform:` block declares no realm serves in its own home
// domain — the default aGB/T 28181 device expects.
func TestApplyPlatformSection_DefaultRealm(t *testing.T) {
	profile, err := model.NewNodeProfile("34020000002000000001", "127.0.0.1:15061", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	got, err := applyPlatformSection(profile, &platformconfig.NodePlatformConfig{}, credstore.New())
	if err != nil {
		t.Fatalf("applyPlatformSection: %v", err)
	}
	serving, _ := got.PlatformServing()
	if serving.Realm() != "3402000000" {
		t.Errorf("realm = %q, want the node's domain", serving.Realm())
	}
	if serving.Policy().Min() != model.DefaultPlatformMinExpires ||
		serving.Policy().Default() != model.DefaultPlatformExpires ||
		serving.Policy().Max() != model.DefaultPlatformMaxExpires {
		t.Errorf("window = %d/%d/%d, want the defaults",
			serving.Policy().Min(), serving.Policy().Default(), serving.Policy().Max())
	}
}

// A section that contradicts itself — a window that is not monotonic, or an
// empty realm — is a start-up error naming the field, never a silently
// unusable platform.
func TestApplyPlatformSection_Invalid(t *testing.T) {
	profile, err := model.NewNodeProfile("34020000002000000001", "127.0.0.1:15061", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	blank := ""
	if _, err := applyPlatformSection(profile, &platformconfig.NodePlatformConfig{Realm: &blank},
		credstore.New()); err == nil {
		t.Error("a blank realm: got nil, want an error")
	}
	if _, err := applyPlatformSection(profile,
		&platformconfig.NodePlatformConfig{Min: 600, Default: 60, Max: 7200}, credstore.New()); err == nil {
		t.Error("a window that is not monotonic: got nil, want an error")
	}
}

// Passwords are configured here, so no error may echo one.
func TestApplyPlatformSection_ErrorsStaySecretFree(t *testing.T) {
	profile, err := model.NewNodeProfile("34020000002000000001", "127.0.0.1:15061", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	accounts := credstore.New()
	if _, err := applyPlatformSection(profile, &platformconfig.NodePlatformConfig{
		Accounts: []platformconfig.NodePlatformAccount{{Username: "34020000011310000001", Password: ""}},
	}, accounts); err == nil {
		t.Fatal("an empty password: got nil, want an error")
	} else if strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks a configured password: %v", err)
	}
}
