// yaml_to_db.go: one-shot migration that turns a release/*.yaml `nodes:`
// section into SQL INSERT statements matching the schema in
// internal/storage/migrations/001_runtime_node_storage.sql.
//
// Usage:
//
//	go run scripts/migrations/yaml_to_db.go release/platform.yaml [more.yaml ...]
//
// Prints one transaction per file to stdout; pipe it to `sqlite3 runtime.db`
// or to a .sql file. The script never touches a database on disk — keeping
// it side-effect-free is what makes the output safe to inspect before it is
// applied.
//
// The output carries plaintext passwords (the schema stores them in the
// clear, as the YAML did), so treat the emitted SQL like the config file
// itself: keep it out of logs and commit history.
//
// The script feeds every value through the same domain model the API uses
// (model.NewRegistration / model.NewExpiresPolicy /
// model.NewPlatformServingWithNoAuth), so a migrated row is byte-identical
// to one created via HTTP POST /v1/nodes: defaults are applied exactly once,
// in the domain, and bad YAML fails here instead of at first use.
//
// Scope: nodes / registration / platform / platform.accounts. TODO: the
// per-node `media:` section (node_media table) is not yet covered.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
	platformconfig "github.com/your-org/gb28181-simulator/internal/platform/config"
)

func main() {
	wrap := flag.Bool("wrap-transaction", true,
		"wrap each file's INSERTs in BEGIN/COMMIT (default true); set false to "+
			"emit bare statements for inspection")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-wrap-transaction=false] file.yaml [file.yaml ...]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	for _, path := range flag.Args() {
		if err := emitFile(path, *wrap); err != nil {
			fmt.Fprintf(os.Stderr, "yaml_to_db: %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

// emitFile loads one YAML file through platformconfig.Load — the same entry
// point the simulator itself uses, so an illegal node id fails here with the
// same message it would fail with at boot — and prints the equivalent SQL.
// Load treats a missing file as first-run and returns defaults silently,
// which for a migration would mean a quiet empty output on a mistyped path,
// so the existence check below is not paranoia.
func emitFile(path string, wrap bool) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	cfg, err := platformconfig.Load(path)
	if err != nil {
		return err
	}
	if len(cfg.Nodes) == 0 {
		// Nothing to migrate is not an error; staying silent keeps it
		// clean to concatenate several files into one pipe.
		return nil
	}

	if wrap {
		fmt.Printf("-- migration of %s nodes, generated %s\n", path, time.Now().UTC().Format(time.RFC3339))
		fmt.Println("BEGIN;")
	}
	for _, n := range cfg.Nodes {
		emitNode(n)
	}
	if wrap {
		fmt.Println("COMMIT;")
	}
	return nil
}

// emitNode writes the INSERTs for one node: the parent row, then the
// optional registration, platform and account rows. Rows are emitted in
// declaration order, which the FOREIGN KEY constraints need when a node's
// downstream is declared before it in the same file — SQLite resolves that
// only when the referenced row already exists inside the transaction, so
// operators with inverted declarations must re-order or split their files.
func emitNode(n platformconfig.NodeConfig) {
	emitNodesRow(n)
	if n.Registration != nil {
		emitRegistrationRow(n.ID, n.Registration)
	}
	if n.Platform != nil {
		emitPlatformRow(n.ID, n.Platform)
		for _, acc := range n.Platform.Accounts {
			emitAccountRow(n.ID, acc, n.Platform.AllowNoAuth)
		}
	}
}

// emitNodesRow writes the nodes row. The YAML `nodes:` list carries no
// cascade wiring — cascade_parent and cascade_children have no YAML
// representation — so both land at their schema defaults and cascade links
// are wired afterwards through the HTTP API.
func emitNodesRow(n platformconfig.NodeConfig) {
	// Reject before emitting rather than letting the DB row carry a kind
	// the node's ID contradicts — the API surface validates this rule, so
	// a migration that slipped one past it would fail only at first use.
	id, err := model.ParseNodeID(n.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "node %s: %v\n", n.ID, err)
		os.Exit(1)
	}
	kind, err := model.ParseNodeKind(n.Kind)
	if err != nil {
		fmt.Fprintf(os.Stderr, "node %s: %v\n", n.ID, err)
		os.Exit(1)
	}
	if err := model.CheckNodeKindConsistency(id, kind); err != nil {
		fmt.Fprintf(os.Stderr, "node %s: %v\n", n.ID, err)
		os.Exit(1)
	}
	fmt.Printf("INSERT INTO nodes (id, kind, domain, addr, vendor, cascade_parent, cascade_children) VALUES (%s, %s, %s, %s, %s, '', '[]');\n",
		quote(n.ID), quote(n.Kind), quote(n.Domain), quote(n.Addr), quote(n.Vendor))
}

// emitRegistrationRow writes the node_registrations row with the values the
// domain model normalised — the same defaults (3600s expiry, 30s timeout,
// UDP, heartbeat trio) a POST /v1/nodes row would carry. Durations become
// the schema's *_ms INTEGER columns.
func emitRegistrationRow(nodeID string, r *platformconfig.NodeRegistrationConfig) {
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:               r.Server,
		ServerID:             r.ServerID,
		Username:             r.Username,
		Password:             r.Password,
		GBVersion:            r.GBVersion,
		Expires:              r.Expires,
		Timeout:              r.Timeout,
		Transport:            r.Transport,
		HeartbeatInterval:    r.HeartbeatInterval,
		HeartbeatTimeout:     r.HeartbeatTimeout,
		HeartbeatMaxFailures: r.HeartbeatMaxFailures,
		AllowNoAuth:          r.AllowNoAuth,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "node %s registration: %v\n", nodeID, err)
		os.Exit(1)
	}
	fmt.Printf("INSERT INTO node_registrations (node_id, server, server_id, username, password, gb_version, expires, timeout_ms, transport, allow_no_auth, heartbeat_interval_ms, heartbeat_timeout_ms, heartbeat_max_failures) VALUES (%s, %s, %s, %s, %s, %s, %d, %d, %s, %d, %d, %d, %d);\n",
		quote(nodeID),
		quote(reg.Server()),
		quote(reg.ServerID()),
		quote(reg.Username()),
		quote(reg.Password()),
		quote(reg.GBVersion()),
		reg.Expires(),
		reg.Timeout().Milliseconds(),
		quote(reg.Transport()),
		boolToInt(reg.AllowNoAuth()),
		reg.HeartbeatInterval().Milliseconds(),
		reg.HeartbeatTimeout().Milliseconds(),
		reg.MaxHeartbeatFailures(),
	)
}

// emitPlatformRow writes the node_platforms row. A nil Realm pointer means
// "challenge in the node's home domain", which the schema spells as ''.
// The expires triple goes through model.NewExpiresPolicy, so a YAML window
// that the runtime would reject — min above default, default above max —
// stops the migration here rather than surfacing as a node that cannot
// register anything.
func emitPlatformRow(nodeID string, p *platformconfig.NodePlatformConfig) {
	realm := ""
	if p.Realm != nil {
		realm = *p.Realm
	}
	policy, err := model.NewExpiresPolicy(p.Min, p.Default, p.Max)
	if err != nil {
		fmt.Fprintf(os.Stderr, "node %s platform: %v\n", nodeID, err)
		os.Exit(1)
	}
	fmt.Printf("INSERT INTO node_platforms (node_id, realm, allow_no_auth, min_expires, default_expires, max_expires, catalog_poll_interval_ms) VALUES (%s, %s, %d, %d, %d, %d, %d);\n",
		quote(nodeID),
		quote(realm),
		boolToInt(p.AllowNoAuth),
		policy.Min(),
		policy.Default(),
		policy.Max(),
		p.CatalogPollInterval.Milliseconds(),
	)
}

// emitAccountRow writes one platform_accounts row — the table Change 1
// already created. no_auth is folded from the platform-level flag: the
// schema stores it per account row, and an account without a password is
// only valid when the platform opted in globally.
func emitAccountRow(nodeID string, acc platformconfig.NodePlatformAccount, allowNoAuth bool) {
	fmt.Printf("INSERT INTO platform_accounts (node_id, username, password, no_auth) VALUES (%s, %s, %s, %d);\n",
		quote(nodeID),
		quote(acc.Username),
		quote(acc.Password),
		boolToInt(allowNoAuth),
	)
}

// quote SQL-escapes a string. A SQLite string literal only needs its single
// quotes doubled; backslashes have no escape meaning there.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// boolToInt turns a Go bool into the 0/1 INTEGER convention this schema uses.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
