package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestUnixSocketConfigMigration(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"legacy", "unix-socket: /tmp/cpa.sock\n", "/tmp/cpa.sock"},
		{"v8", "config-version: 8\nserver:\n  unix-socket: /tmp/cpa.sock\n", "/tmp/cpa.sock"},
		{"v8 overrides legacy", "unix-socket: /tmp/old.sock\nserver:\n  unix-socket: /tmp/new.sock\n", "/tmp/new.sock"},
		{"v8 disables legacy", "unix-socket: /tmp/old.sock\nserver:\n  unix-socket: \"\"\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseConfigBytes([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.UnixSocket != tc.want {
				t.Fatalf("UnixSocket = %q, want %q", cfg.UnixSocket, tc.want)
			}
			migrated, _, err := NormalizeConfigLayout([]byte(tc.raw), true)
			if err != nil {
				t.Fatal(err)
			}
			if err = ValidateV8Config(migrated); err != nil {
				t.Fatal(err)
			}
			var doc yaml.Node
			if err = yaml.Unmarshal(migrated, &doc); err != nil {
				t.Fatal(err)
			}
			if yamlPath(doc.Content[0], "unix-socket") != nil {
				t.Fatal("migration retained the legacy unix-socket field")
			}
			value := yamlPath(doc.Content[0], "server.unix-socket")
			if value == nil || value.Value != tc.want {
				t.Fatalf("migration lost server.unix-socket: %s", migrated)
			}
			reloaded, err := ParseConfigBytes(migrated)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.UnixSocket != tc.want {
				t.Fatalf("migrated UnixSocket = %q, want %q", reloaded.UnixSocket, tc.want)
			}
		})
	}
}
