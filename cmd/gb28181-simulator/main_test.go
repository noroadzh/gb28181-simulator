package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/adapter/credstore"
	"github.com/your-org/gb28181-simulator/internal/adapter/nodesql"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
	"github.com/your-org/gb28181-simulator/internal/storage"
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

// allow_no_auth=true lets a platform accept a downstream that registered
// without a password; the resulting credential MUST carry the no-auth flag
// so the AuthenticatorAdapter.Verify shortcut fires.
func TestApplyPlatformSection_AllowNoAuthEmptyPasswordOK(t *testing.T) {
	profile, err := model.NewNodeProfile("34020000002000000001", "127.0.0.1:15061", "3402000000", "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	accounts := credstore.New()
	if _, err := applyPlatformSection(profile, &platformconfig.NodePlatformConfig{
		AllowNoAuth: true,
		Accounts: []platformconfig.NodePlatformAccount{
			{Username: "34020000011310000001", Password: ""},
		},
	}, accounts); err != nil {
		t.Fatalf("allow_no_auth with empty password: %v", err)
	}
	cred, ok := accounts.Lookup(profile.ID(), "34020000011310000001")
	if !ok {
		t.Fatal("credential not added to store")
	}
	if !cred.NoAuth() {
		t.Fatal("stored credential does not carry the no-auth flag")
	}
}

// A node block in YAML survives the one-shot import byte for byte: the
// registration credentials and the serving window a user wrote down are the
// ones the store ends up holding, or the import is worthless as a migration.
func TestImportYAMLNodes_RoundTripsEveryField(t *testing.T) {
	store := newTestNodeStore(t)
	realm := "3402000001"
	err := importYAMLNodes(store, []platformconfig.NodeConfig{{
		ID:     "34020000002000000001",
		Kind:   "platform-large",
		Domain: "3402000000",
		Addr:   "127.0.0.1:15061",
		Vendor: "acme",
		Registration: &platformconfig.NodeRegistrationConfig{
			Server:    "10.10.0.1:5060",
			ServerID:  "34020000001320000001",
			Username:  "34020000002000000001",
			Password:  "change-me",
			GBVersion: "2022",
			Expires:   7200,
		},
		Platform: &platformconfig.NodePlatformConfig{
			Realm:   &realm,
			Min:     60,
			Default: 1800,
			Max:     7200,
		},
	}})
	if err != nil {
		t.Fatalf("importYAMLNodes: %v", err)
	}

	wantID, err := model.ParseNodeID("34020000002000000001")
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	got, err := store.GetNode(context.Background(), wantID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Kind != model.NodeKindPlatformLarge {
		t.Errorf("kind = %v, want platform-large", got.Kind)
	}
	if got.Addr != "127.0.0.1:15061" || got.Domain != "3402000000" || got.Vendor != "acme" {
		t.Errorf("identity = %q/%q/%q, want the configured addr/domain/vendor",
			got.Addr, got.Domain, got.Vendor)
	}
	if !got.HasRegistration {
		t.Fatal("the registration was dropped")
	}
	reg := got.Registration
	if reg.Server() != "10.10.0.1:5060" || reg.ServerID() != "34020000001320000001" {
		t.Errorf("upstream = %q/%q, want the configured server/server_id",
			reg.Server(), reg.ServerID())
	}
	if reg.Username() != "34020000002000000001" {
		t.Errorf("username = %q, want the configured one", reg.Username())
	}
	if reg.GBVersion() != "2022" || reg.Expires() != 7200 {
		t.Errorf("gb_version/expires = %q/%d, want 2022/7200", reg.GBVersion(), reg.Expires())
	}
	if !got.HasPlatform {
		t.Fatal("the platform section was dropped")
	}
	plat := got.Platform
	if plat.Realm() != realm || plat.Policy().Default() != 1800 {
		t.Errorf("platform = %+v, want realm %q with 1800 as the default lifetime", plat, realm)
	}
}

// A YAML file that carries no `nodes:` section is not an error: it imports
// nothing and leaves the store exactly as empty as it found it.
func TestImportYAMLNodes_EmptyIsNotAnError(t *testing.T) {
	store := newTestNodeStore(t)
	if err := importYAMLNodes(store, nil); err != nil {
		t.Fatalf("importYAMLNodes(nil): %v", err)
	}
	nodes, err := store.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("got %d nodes, want 0", len(nodes))
	}
}

// A block the store cannot hold must fail the import by naming the offending
// index, not silently skip it: a half-imported configuration would start with
// nodes missing and no clue which.
func TestImportYAMLNodes_InvalidFailsAndNamesTheField(t *testing.T) {
	cases := []struct {
		name  string
		nodes []platformconfig.NodeConfig
		want  string
	}{
		{
			name:  "a malformed id",
			nodes: []platformconfig.NodeConfig{{ID: "not-a-number", Kind: "device"}},
			want:  "node[0].id",
		},
		{
			name:  "an unknown kind",
			nodes: []platformconfig.NodeConfig{{ID: "34020000002000000001", Kind: "hub"}},
			want:  "node[0].kind",
		},
		{
			name: "a non-monotonic window",
			// The id must agree with the kind, or the consistency
			// check fires before the window is ever examined.
			nodes: []platformconfig.NodeConfig{{
				ID: "34020000002160000001", Kind: "platform-small", Domain: "3402000000",
				Addr:     "127.0.0.1:15070",
				Platform: &platformconfig.NodePlatformConfig{Min: 600, Default: 60, Max: 7200},
			}},
			want: "node[0].platform",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestNodeStore(t)
			err := importYAMLNodes(store, tc.nodes)
			if err == nil {
				t.Fatal("got nil, want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// An import that stops halfway leaves no orphan rows behind for the next
// start-up to trip over.
func TestImportYAMLNodes_StopsAtTheFirstBadNode(t *testing.T) {
	store := newTestNodeStore(t)
	err := importYAMLNodes(store, []platformconfig.NodeConfig{
		{ID: "34020000001310000001", Kind: "device", Domain: "3402000000", Addr: "127.0.0.1:15070"},
		{ID: "broken", Kind: "device", Domain: "3402000000", Addr: "127.0.0.1:15071"},
	})
	if err == nil {
		t.Fatal("got nil, want an error")
	}
	nodes, listErr := store.ListNodes(context.Background())
	if listErr != nil {
		t.Fatalf("ListNodes: %v", listErr)
	}
	if len(nodes) != 1 {
		t.Errorf("the store holds %d nodes, want the 1 that imported cleanly", len(nodes))
	}
}

// newTestNodeStore builds a store over a throwaway database with the schema
// migrations already applied — the same path production takes through
// storage.Bootstrap, minus the file.
func newTestNodeStore(t *testing.T) *nodesql.Store {
	t.Helper()
	db, err := storage.Bootstrap(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return nodesql.New(db)
}
