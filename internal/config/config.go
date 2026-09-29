package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds notty's runtime configuration (spec §10), layered from
// built-in defaults, the vault's .notty/settings.toml, and the local
// config file.
type Config struct {
	Vault       string
	Theme       string
	Vim         bool
	LineNumbers bool
	Editor      string
	AutosaveMS  int

	Sync struct {
		Enabled        bool
		CommitDelayS   int
		FetchIntervalM int
	} `toml:"sync"`

	Trash struct {
		RetentionDays int
	} `toml:"trash"`

	Tasks struct {
		ShowDone    bool
		DueSoonDays int
	} `toml:"tasks"`

	Images struct {
		Protocol    string
		MaxImportMB int
	} `toml:"images"`
}

// Default returns notty's built-in configuration defaults (spec §10).
func Default() Config {
	var c Config
	c.Vault = "~/Notes"
	c.Theme = "catppuccin-mocha"
	c.Vim = true
	c.LineNumbers = false
	c.Editor = ""
	c.AutosaveMS = 1000
	c.Sync.Enabled = true
	c.Sync.CommitDelayS = 5
	c.Sync.FetchIntervalM = 5
	c.Trash.RetentionDays = 30
	c.Tasks.ShowDone = false
	c.Tasks.DueSoonDays = 7
	c.Images.Protocol = "auto"
	c.Images.MaxImportMB = 5
	return c
}

// VaultPath returns the vault path with a leading "~" expanded to the
// user's home directory.
func (c Config) VaultPath() string {
	return ExpandHome(c.Vault)
}

// EditorCommand resolves the editor to launch: the configured Editor,
// then $VISUAL, then $EDITOR, then "nano" ("notepad" on Windows). getenv
// and goos are injected for testability (use os.Getenv and runtime.GOOS
// in production code).
func (c Config) EditorCommand(getenv func(string) string, goos string) string {
	if c.Editor != "" {
		return c.Editor
	}
	if v := getenv("VISUAL"); v != "" {
		return v
	}
	if v := getenv("EDITOR"); v != "" {
		return v
	}
	if goos == "windows" {
		return "notepad"
	}
	return "nano"
}

// overlay mirrors Config with every field as a pointer, so that decoding a
// partial TOML file leaves absent keys nil. This distinguishes "not set in
// this file" from an explicit zero value (e.g. `vim = false`), which is
// essential for correct layering.
type overlay struct {
	Vault       *string
	Theme       *string
	Vim         *bool
	LineNumbers *bool `toml:"line_numbers"`
	Editor      *string
	AutosaveMS  *int `toml:"autosave_ms"`

	Sync struct {
		Enabled        *bool `toml:"enabled"`
		CommitDelayS   *int  `toml:"commit_delay_s"`
		FetchIntervalM *int  `toml:"fetch_interval_m"`
	} `toml:"sync"`

	Trash struct {
		RetentionDays *int `toml:"retention_days"`
	} `toml:"trash"`

	Tasks struct {
		ShowDone    *bool `toml:"show_done"`
		DueSoonDays *int  `toml:"due_soon_days"`
	} `toml:"tasks"`

	Images struct {
		Protocol    *string `toml:"protocol"`
		MaxImportMB *int    `toml:"max_import_mb"`
	} `toml:"images"`
}

// applyOverlay copies every non-nil field of ov onto c, leaving fields
// absent from the source file untouched.
func applyOverlay(c *Config, ov overlay) {
	if ov.Vault != nil {
		c.Vault = *ov.Vault
	}
	if ov.Theme != nil {
		c.Theme = *ov.Theme
	}
	if ov.Vim != nil {
		c.Vim = *ov.Vim
	}
	if ov.LineNumbers != nil {
		c.LineNumbers = *ov.LineNumbers
	}
	if ov.Editor != nil {
		c.Editor = *ov.Editor
	}
	if ov.AutosaveMS != nil {
		c.AutosaveMS = *ov.AutosaveMS
	}
	if ov.Sync.Enabled != nil {
		c.Sync.Enabled = *ov.Sync.Enabled
	}
	if ov.Sync.CommitDelayS != nil {
		c.Sync.CommitDelayS = *ov.Sync.CommitDelayS
	}
	if ov.Sync.FetchIntervalM != nil {
		c.Sync.FetchIntervalM = *ov.Sync.FetchIntervalM
	}
	if ov.Trash.RetentionDays != nil {
		c.Trash.RetentionDays = *ov.Trash.RetentionDays
	}
	if ov.Tasks.ShowDone != nil {
		c.Tasks.ShowDone = *ov.Tasks.ShowDone
	}
	if ov.Tasks.DueSoonDays != nil {
		c.Tasks.DueSoonDays = *ov.Tasks.DueSoonDays
	}
	if ov.Images.Protocol != nil {
		c.Images.Protocol = *ov.Images.Protocol
	}
	if ov.Images.MaxImportMB != nil {
		c.Images.MaxImportMB = *ov.Images.MaxImportMB
	}
}

// loadOverlay reads and decodes the TOML file at path. A missing file is
// not an error: it yields a zero-value (all-nil) overlay.
func loadOverlay(path string) (overlay, error) {
	var ov overlay
	if path == "" {
		return ov, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ov, nil
		}
		return ov, fmt.Errorf("reading config %s: %w", path, err)
	}
	if _, err := toml.Decode(string(data), &ov); err != nil {
		return ov, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return ov, nil
}

// Load builds a Config by layering, in order: built-in defaults, the
// vault's .notty/settings.toml (only if vaultRoot is non-empty), and the
// local config file at localPath. Missing files are fine; only keys
// actually present in a file override the layer beneath it.
func Load(localPath string, vaultRoot string) (Config, error) {
	c := Default()

	if vaultRoot != "" {
		vaultOv, err := loadOverlay(filepath.Join(vaultRoot, ".notty", "settings.toml"))
		if err != nil {
			return Config{}, err
		}
		applyOverlay(&c, vaultOv)
	}

	localOv, err := loadOverlay(localPath)
	if err != nil {
		return Config{}, err
	}
	applyOverlay(&c, localOv)

	return c, nil
}

// SetKey rewrites exactly one key in the local config file at localPath,
// creating the file (and its parent directory) if it doesn't exist. key is
// either a bare top-level key ("theme") or a dotted key into a table
// ("sync.enabled"). Every other key already present in the file is left
// untouched.
func SetKey(localPath string, key string, value any) error {
	tree, err := readTOMLTree(localPath)
	if err != nil {
		return err
	}

	parts := strings.Split(key, ".")
	node := tree
	for _, p := range parts[:len(parts)-1] {
		child, ok := node[p].(map[string]any)
		if !ok {
			child = map[string]any{}
			node[p] = child
		}
		node = child
	}
	node[parts[len(parts)-1]] = value

	return writeTOMLTree(localPath, tree)
}

func readTOMLTree(path string) (map[string]any, error) {
	tree := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tree, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return tree, nil
}

func writeTOMLTree(path string, tree map[string]any) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating config directory %s: %w", dir, err)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating config %s: %w", path, err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(tree); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}
