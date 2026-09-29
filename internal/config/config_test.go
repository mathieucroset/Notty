package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()

	if c.Vault != "~/Notes" {
		t.Errorf("Vault = %q, want %q", c.Vault, "~/Notes")
	}
	if c.Theme != "catppuccin-mocha" {
		t.Errorf("Theme = %q, want %q", c.Theme, "catppuccin-mocha")
	}
	if !c.Vim {
		t.Error("Vim = false, want true")
	}
	if c.LineNumbers {
		t.Error("LineNumbers = true, want false")
	}
	if c.Editor != "" {
		t.Errorf("Editor = %q, want empty", c.Editor)
	}
	if c.AutosaveMS != 1000 {
		t.Errorf("AutosaveMS = %d, want 1000", c.AutosaveMS)
	}
	if !c.Sync.Enabled {
		t.Error("Sync.Enabled = false, want true")
	}
	if c.Sync.CommitDelayS != 5 {
		t.Errorf("Sync.CommitDelayS = %d, want 5", c.Sync.CommitDelayS)
	}
	if c.Sync.FetchIntervalM != 5 {
		t.Errorf("Sync.FetchIntervalM = %d, want 5", c.Sync.FetchIntervalM)
	}
	if c.Trash.RetentionDays != 30 {
		t.Errorf("Trash.RetentionDays = %d, want 30", c.Trash.RetentionDays)
	}
	if c.Tasks.ShowDone {
		t.Error("Tasks.ShowDone = true, want false")
	}
	if c.Tasks.DueSoonDays != 7 {
		t.Errorf("Tasks.DueSoonDays = %d, want 7", c.Tasks.DueSoonDays)
	}
	if c.Images.Protocol != "auto" {
		t.Errorf("Images.Protocol = %q, want %q", c.Images.Protocol, "auto")
	}
	if c.Images.MaxImportMB != 5 {
		t.Errorf("Images.MaxImportMB = %d, want 5", c.Images.MaxImportMB)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func TestLoad(t *testing.T) {
	t.Run("missing files fall back to defaults", func(t *testing.T) {
		dir := t.TempDir()
		c, err := Load(filepath.Join(dir, "nope.toml"), "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := Default()
		if c != want {
			t.Errorf("Load() = %+v, want defaults %+v", c, want)
		}
	})

	t.Run("missing vault settings file is fine", func(t *testing.T) {
		dir := t.TempDir()
		vaultRoot := filepath.Join(dir, "vault")
		if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		c, err := Load(filepath.Join(dir, "nope.toml"), vaultRoot)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c != Default() {
			t.Errorf("Load() = %+v, want defaults", c)
		}
	})

	t.Run("local overrides defaults", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `theme = "solarized"
vim = false

[tasks]
show_done = true
`)
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Theme != "solarized" {
			t.Errorf("Theme = %q, want solarized", c.Theme)
		}
		if c.Vim {
			t.Error("Vim = true, want false (local override of default true)")
		}
		if !c.Tasks.ShowDone {
			t.Error("Tasks.ShowDone = false, want true")
		}
		// Untouched keys keep their defaults.
		if c.AutosaveMS != 1000 {
			t.Errorf("AutosaveMS = %d, want default 1000", c.AutosaveMS)
		}
		if c.Vault != "~/Notes" {
			t.Errorf("Vault = %q, want default", c.Vault)
		}
	})

	t.Run("vault overrides defaults, local overrides vault", func(t *testing.T) {
		dir := t.TempDir()
		vaultRoot := filepath.Join(dir, "vault")
		vaultSettings := filepath.Join(vaultRoot, ".notty", "settings.toml")
		writeFile(t, vaultSettings, `theme = "vault-theme"
vim = false

[sync]
enabled = false
`)
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `theme = "local-theme"
`)

		c, err := Load(local, vaultRoot)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		// Local wins over vault.
		if c.Theme != "local-theme" {
			t.Errorf("Theme = %q, want local-theme", c.Theme)
		}
		// Vault wins over default where local doesn't set it.
		if c.Vim {
			t.Error("Vim = true, want false (vault override)")
		}
		if c.Sync.Enabled {
			t.Error("Sync.Enabled = true, want false (vault override)")
		}
		// Untouched keys keep defaults.
		if c.Sync.CommitDelayS != 5 {
			t.Errorf("Sync.CommitDelayS = %d, want default 5", c.Sync.CommitDelayS)
		}
	})

	t.Run("bool false in local overrides true default without vault", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `vim = false
`)
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Vim {
			t.Error("Vim = true, want false")
		}
	})

	t.Run("absent key does not reset to zero value", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		// Only sets one unrelated key; Vim must remain the default (true).
		writeFile(t, local, `theme = "x"
`)
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !c.Vim {
			t.Error("Vim = false, want true (unset key should keep default)")
		}
	})

	t.Run("invalid toml in local file returns error naming file", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `this is not = = valid toml [[[`)
		_, err := Load(local, "")
		if err == nil {
			t.Fatal("Load: want error, got nil")
		}
		if !strings.Contains(err.Error(), local) {
			t.Errorf("Load error = %v, want it to mention %q", err, local)
		}
	})

	t.Run("invalid toml in vault settings returns error naming file", func(t *testing.T) {
		dir := t.TempDir()
		vaultRoot := filepath.Join(dir, "vault")
		vaultSettings := filepath.Join(vaultRoot, ".notty", "settings.toml")
		writeFile(t, vaultSettings, `not [[[ valid`)
		_, err := Load(filepath.Join(dir, "nope.toml"), vaultRoot)
		if err == nil {
			t.Fatal("Load: want error, got nil")
		}
		if !strings.Contains(err.Error(), vaultSettings) {
			t.Errorf("Load error = %v, want it to mention %q", err, vaultSettings)
		}
	})
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tilde alone", "~", home},
		{"tilde slash path", "~/Notes", filepath.Join(home, "Notes")},
		{"tilde slash nested", "~/a/b/c", filepath.Join(home, "a", "b", "c")},
		{"absolute path unchanged", "/tmp/foo", "/tmp/foo"},
		{"relative path unchanged", "relative/path", "relative/path"},
		{"tilde in middle unchanged", "/foo/~/bar", "/foo/~/bar"},
		{"tildename unchanged", "~someuser/x", "~someuser/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandHome(tt.in)
			if got != tt.want {
				t.Errorf("ExpandHome(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestVaultPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	c := Default()
	want := filepath.Join(home, "Notes")
	if got := c.VaultPath(); got != want {
		t.Errorf("VaultPath() = %q, want %q", got, want)
	}
}

func TestSetKey(t *testing.T) {
	t.Run("creates file when missing", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "sub", "config.toml")
		if err := SetKey(local, "theme", "dracula"); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Theme != "dracula" {
			t.Errorf("Theme = %q, want dracula", c.Theme)
		}
	})

	t.Run("top-level key preserves other keys", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `theme = "solarized"
vim = false
`)
		if err := SetKey(local, "theme", "dracula"); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Theme != "dracula" {
			t.Errorf("Theme = %q, want dracula", c.Theme)
		}
		if c.Vim {
			t.Error("Vim = true, want false (should be preserved)")
		}
	})

	t.Run("dotted key sets nested table and preserves siblings", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `[sync]
enabled = true
commit_delay_s = 5
`)
		if err := SetKey(local, "sync.enabled", false); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Sync.Enabled {
			t.Error("Sync.Enabled = true, want false")
		}
		if c.Sync.CommitDelayS != 5 {
			t.Errorf("Sync.CommitDelayS = %d, want 5 (preserved)", c.Sync.CommitDelayS)
		}
	})

	t.Run("dotted key creates table when absent", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		if err := SetKey(local, "tasks.due_soon_days", 3); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Tasks.DueSoonDays != 3 {
			t.Errorf("Tasks.DueSoonDays = %d, want 3", c.Tasks.DueSoonDays)
		}
	})

	t.Run("bool value round-trips", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		if err := SetKey(local, "vim", false); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Vim {
			t.Error("Vim = true, want false")
		}
	})

	t.Run("valid dotted key is accepted and applied", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		if err := SetKey(local, "images.max_import_mb", 42); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Images.MaxImportMB != 42 {
			t.Errorf("Images.MaxImportMB = %d, want 42", c.Images.MaxImportMB)
		}
	})

	t.Run("rejects unknown top-level key", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		err := SetKey(local, "bogus", "x")
		if err == nil {
			t.Fatal("SetKey: want error, got nil")
		}
		if !strings.Contains(err.Error(), "bogus") {
			t.Errorf("SetKey error = %v, want it to mention the key %q", err, "bogus")
		}
		if _, statErr := os.Stat(local); !os.IsNotExist(statErr) {
			t.Error("SetKey: rejected key should not create the config file")
		}
	})

	t.Run("rejects unknown dotted key under a known table", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `[sync]
enabled = true
`)
		err := SetKey(local, "sync.bogus", true)
		if err == nil {
			t.Fatal("SetKey: want error, got nil")
		}
		if !strings.Contains(err.Error(), "sync.bogus") {
			t.Errorf("SetKey error = %v, want it to mention the key %q", err, "sync.bogus")
		}
		// The existing file must be untouched by the rejected write.
		c, loadErr := Load(local, "")
		if loadErr != nil {
			t.Fatalf("Load: %v", loadErr)
		}
		if !c.Sync.Enabled {
			t.Error("Sync.Enabled = false, want true (file should be unmodified)")
		}
	})

	t.Run("rejects key from the wrong table", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		err := SetKey(local, "vault.enabled", true)
		if err == nil {
			t.Fatal("SetKey: want error, got nil")
		}
	})

	t.Run("preserves comments and key order for an untouched top-level key", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		original := `# personal config
vim = false   # I like vim off
theme = "solarized"

[sync]
enabled = true
`
		writeFile(t, local, original)
		if err := SetKey(local, "vim", true); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		got, err := os.ReadFile(local)
		if err != nil {
			t.Fatal(err)
		}
		want := `# personal config
vim = true   # I like vim off
theme = "solarized"

[sync]
enabled = true
`
		if string(got) != want {
			t.Errorf("file =\n%s\nwant\n%s", got, want)
		}
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !c.Vim {
			t.Error("Vim = false, want true")
		}
	})

	t.Run("preserves trailing comment on a replaced dotted key line", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `[trash]
retention_days = 30 # days to keep deleted notes
`)
		if err := SetKey(local, "trash.retention_days", 7); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		got, err := os.ReadFile(local)
		if err != nil {
			t.Fatal(err)
		}
		want := `[trash]
retention_days = 7 # days to keep deleted notes
`
		if string(got) != want {
			t.Errorf("file =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("inserts a new key into an existing table without disturbing siblings", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `[tasks]
show_done = true

[images]
protocol = "auto"
`)
		if err := SetKey(local, "tasks.due_soon_days", 3); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		got, err := os.ReadFile(local)
		if err != nil {
			t.Fatal(err)
		}
		want := `[tasks]
show_done = true
due_soon_days = 3

[images]
protocol = "auto"
`
		if string(got) != want {
			t.Errorf("file =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("creates a missing table at the end of the file", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `theme = "dracula"
`)
		if err := SetKey(local, "images.max_import_mb", 10); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		got, err := os.ReadFile(local)
		if err != nil {
			t.Fatal(err)
		}
		want := `theme = "dracula"

[images]
max_import_mb = 10
`
		if string(got) != want {
			t.Errorf("file =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("inserts a new top-level key before the first table header", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `# leading comment
vim = false

[sync]
enabled = true
`)
		if err := SetKey(local, "theme", "nord"); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		got, err := os.ReadFile(local)
		if err != nil {
			t.Fatal(err)
		}
		want := `# leading comment
vim = false
theme = "nord"

[sync]
enabled = true
`
		if string(got) != want {
			t.Errorf("file =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("string values are quoted and escaped", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		if err := SetKey(local, "editor", `vim "with quotes" and \backslash\`+"\tand tab"); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		got, err := os.ReadFile(local)
		if err != nil {
			t.Fatal(err)
		}
		want := "editor = \"vim \\\"with quotes\\\" and \\\\backslash\\\\\\tand tab\"\n"
		if string(got) != want {
			t.Errorf("file =\n%q\nwant\n%q", got, want)
		}
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if want := `vim "with quotes" and \backslash\` + "\tand tab"; c.Editor != want {
			t.Errorf("Editor = %q, want %q", c.Editor, want)
		}
	})

	t.Run("file still parses after repeated edits", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		writeFile(t, local, `# config
theme = "dracula" # was set via palette
vim = true

[sync]
enabled = true
commit_delay_s = 5

[tasks]
show_done = false
`)
		if err := SetKey(local, "theme", "nord"); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		if err := SetKey(local, "sync.enabled", false); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		if err := SetKey(local, "tasks.due_soon_days", 14); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		if err := SetKey(local, "images.protocol", "kitty"); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		c, err := Load(local, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.Theme != "nord" {
			t.Errorf("Theme = %q, want nord", c.Theme)
		}
		if c.Sync.Enabled {
			t.Error("Sync.Enabled = true, want false")
		}
		if c.Sync.CommitDelayS != 5 {
			t.Errorf("Sync.CommitDelayS = %d, want 5 (preserved)", c.Sync.CommitDelayS)
		}
		if c.Tasks.DueSoonDays != 14 {
			t.Errorf("Tasks.DueSoonDays = %d, want 14", c.Tasks.DueSoonDays)
		}
		if c.Tasks.ShowDone {
			t.Error("Tasks.ShowDone = true, want false (preserved)")
		}
		if c.Images.Protocol != "kitty" {
			t.Errorf("Images.Protocol = %q, want kitty", c.Images.Protocol)
		}
		got, err := os.ReadFile(local)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), `theme = "nord" # was set via palette`) {
			t.Errorf("expected theme's trailing comment to survive the value replacement, got:\n%s", got)
		}
	})

	t.Run("writes atomically, leaving no temp file behind", func(t *testing.T) {
		dir := t.TempDir()
		local := filepath.Join(dir, "config.toml")
		if err := SetKey(local, "theme", "dracula"); err != nil {
			t.Fatalf("SetKey: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "config.toml" {
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name()
			}
			t.Errorf("dir entries = %v, want only config.toml", names)
		}
	})
}

func TestEditorCommand(t *testing.T) {
	tests := []struct {
		name   string
		editor string
		env    map[string]string
		goos   string
		want   string
	}{
		{
			name:   "explicit editor wins",
			editor: "vim",
			env:    map[string]string{"VISUAL": "emacs", "EDITOR": "nano"},
			goos:   "linux",
			want:   "vim",
		},
		{
			name:   "falls back to VISUAL",
			editor: "",
			env:    map[string]string{"VISUAL": "emacs", "EDITOR": "nano"},
			goos:   "linux",
			want:   "emacs",
		},
		{
			name:   "falls back to EDITOR",
			editor: "",
			env:    map[string]string{"EDITOR": "nano-classic"},
			goos:   "linux",
			want:   "nano-classic",
		},
		{
			name:   "falls back to nano on non-windows",
			editor: "",
			env:    map[string]string{},
			goos:   "darwin",
			want:   "nano",
		},
		{
			name:   "falls back to notepad on windows",
			editor: "",
			env:    map[string]string{},
			goos:   "windows",
			want:   "notepad",
		},
		{
			name:   "empty VISUAL is treated as unset",
			editor: "",
			env:    map[string]string{"VISUAL": "", "EDITOR": "nano-classic"},
			goos:   "linux",
			want:   "nano-classic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			c := Config{Editor: tt.editor}
			got := c.EditorCommand(getenv, tt.goos)
			if got != tt.want {
				t.Errorf("EditorCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigDirStateDirXDG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG env vars are not used on windows")
	}

	t.Run("ConfigDir honors XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/xdg-config")
		if got, want := ConfigDir(), filepath.Join("/xdg-config", "notty"); got != want {
			t.Errorf("ConfigDir() = %q, want %q", got, want)
		}
	})

	t.Run("StateDir honors XDG_STATE_HOME", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "/xdg-state")
		if got, want := StateDir(), filepath.Join("/xdg-state", "notty"); got != want {
			t.Errorf("StateDir() = %q, want %q", got, want)
		}
	})

	t.Run("ConfigDir falls back to ~/.config/notty", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(home, ".config", "notty")
		if got := ConfigDir(); got != want {
			t.Errorf("ConfigDir() = %q, want %q", got, want)
		}
	})

	t.Run("StateDir falls back to ~/.local/state/notty", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "")
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(home, ".local", "state", "notty")
		if got := StateDir(); got != want {
			t.Errorf("StateDir() = %q, want %q", got, want)
		}
	})

	t.Run("ConfigPath is ConfigDir/config.toml", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/xdg-config")
		want := filepath.Join("/xdg-config", "notty", "config.toml")
		if got := ConfigPath(); got != want {
			t.Errorf("ConfigPath() = %q, want %q", got, want)
		}
	})
}
