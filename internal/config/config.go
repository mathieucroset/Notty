package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds notty's runtime configuration (spec §10), layered from
// built-in defaults, the vault's .notty/settings.toml, and the local
// config file. Editor is local only (see Load).
type Config struct {
	Vault       string
	Theme       string
	Vim         bool
	LineNumbers bool `toml:"line_numbers"`
	Editor      string
	AutosaveMS  int `toml:"autosave_ms"`
	// Icons is the glyph set the UI draws with: "nerd" (needs a Nerd
	// Font), "unicode" (the default), or "ascii".
	Icons string `toml:"icons"`
	// UpdateCheck lets the app check, at most once a day, whether a newer
	// release is published.
	UpdateCheck bool `toml:"update_check"`

	Sync struct {
		Enabled        bool `toml:"enabled"`
		CommitDelayS   int  `toml:"commit_delay_s"`
		FetchIntervalM int  `toml:"fetch_interval_m"`
	} `toml:"sync"`

	Trash struct {
		RetentionDays int `toml:"retention_days"`
	} `toml:"trash"`

	Tasks struct {
		ShowDone    bool `toml:"show_done"`
		DueSoonDays int  `toml:"due_soon_days"`
	} `toml:"tasks"`

	Images struct {
		Protocol    string `toml:"protocol"`
		MaxImportMB int    `toml:"max_import_mb"`
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
	c.Icons = "unicode"
	c.UpdateCheck = true
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
	AutosaveMS  *int    `toml:"autosave_ms"`
	Icons       *string `toml:"icons"`
	UpdateCheck *bool   `toml:"update_check"`

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
	if ov.Icons != nil {
		c.Icons = *ov.Icons
	}
	if ov.UpdateCheck != nil {
		c.UpdateCheck = *ov.UpdateCheck
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
//
// The editor key is taken from the local file only: the vault's settings
// travel through git, and whoever can push to a shared vault must not
// choose the command Notty runs. An editor set there is ignored and
// logged.
func Load(localPath string, vaultRoot string) (Config, error) {
	c := Default()

	if vaultRoot != "" {
		settings := filepath.Join(vaultRoot, ".notty", "settings.toml")
		vaultOv, err := loadOverlay(settings)
		if err != nil {
			return Config{}, err
		}
		if vaultOv.Editor != nil {
			slog.Warn("config: ignoring the editor set in the vault settings; set it in the local config file",
				"file", settings)
			vaultOv.Editor = nil
		}
		applyOverlay(&c, vaultOv)
	}

	localOv, err := loadOverlay(localPath)
	if err != nil {
		return Config{}, err
	}
	applyOverlay(&c, localOv)

	if !slices.Contains(IconSets, c.Icons) {
		return Config{}, fmt.Errorf("config: icons must be one of %s, not %q", strings.Join(IconSets, ", "), c.Icons)
	}
	return c, nil
}

// IconSets are the accepted values of the icons key, in the order the
// README lists them.
var IconSets = []string{"nerd", "unicode", "ascii"}

// validConfigKeys is the exact set of keys SetKey accepts, matching the
// Config schema (spec §10): bare top-level keys, and dotted "table.field"
// keys for the nested sections.
var validConfigKeys = map[string]bool{
	"vault":                 true,
	"theme":                 true,
	"vim":                   true,
	"line_numbers":          true,
	"editor":                true,
	"autosave_ms":           true,
	"icons":                 true,
	"update_check":          true,
	"sync.enabled":          true,
	"sync.commit_delay_s":   true,
	"sync.fetch_interval_m": true,
	"trash.retention_days":  true,
	"tasks.show_done":       true,
	"tasks.due_soon_days":   true,
	"images.protocol":       true,
	"images.max_import_mb":  true,
}

// SetKey rewrites exactly one key in the local config file at localPath,
// creating the file (and its parent directory) if it doesn't exist. key is
// either a bare top-level key ("theme") or a dotted key into a table
// ("sync.enabled"). It is a targeted text edit: every other line in the
// file (comments, key order, other keys) is preserved byte-for-byte. key
// must be one of the known config keys; anything else returns an error and
// leaves the file untouched.
func SetKey(localPath string, key string, value any) error {
	if !validConfigKeys[key] {
		return fmt.Errorf("config: unknown key %q", key)
	}

	encoded, err := encodeTOMLValue(value)
	if err != nil {
		return fmt.Errorf("config: encoding value for %q: %w", key, err)
	}

	lines, err := readConfigLines(localPath)
	if err != nil {
		return err
	}

	parts := strings.SplitN(key, ".", 2)
	var newLines []string
	if len(parts) == 1 {
		newLines = setTopLevelKey(lines, parts[0], encoded)
	} else {
		newLines = setTableKey(lines, parts[0], parts[1], encoded)
	}

	content := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		content += "\n"
	}

	return writeConfigFile(localPath, content)
}

// readConfigLines reads path and splits it into lines with any line
// endings stripped. A missing file yields no lines.
func readConfigLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

// tomlHeaderLineRe matches any table header line ("[sync]", "[[foo]]",
// possibly indented or with a trailing comment), used to find section
// boundaries.
var tomlHeaderLineRe = regexp.MustCompile(`^\s*\[`)

// tableHeaderRe returns a regexp matching the exact header line for a
// single top-level table, e.g. "[sync]" (not "[[sync]]" or "[sync.sub]").
func tableHeaderRe(table string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*\[\s*` + regexp.QuoteMeta(table) + `\s*\]\s*(#.*)?$`)
}

// fieldLineRe returns a regexp matching a "field = value" assignment line
// for the exact bare key field, capturing the leading indent, the
// whitespace-and-equals separator, and the remainder of the line (value
// plus any trailing comment).
func fieldLineRe(field string) *regexp.Regexp {
	return regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(field) + `(\s*=\s*)(.*)$`)
}

// setTopLevelKey replaces or inserts a bare top-level key. The search (and
// insertion point, when the key is absent) is restricted to the lines
// before the first table header, per spec §8.
func setTopLevelKey(lines []string, field, encoded string) []string {
	boundary := len(lines)
	for i, l := range lines {
		if tomlHeaderLineRe.MatchString(l) {
			boundary = i
			break
		}
	}

	re := fieldLineRe(field)
	for i := 0; i < boundary; i++ {
		if m := re.FindStringSubmatch(lines[i]); m != nil {
			lines[i] = replaceFieldValue(m, field, encoded)
			return lines
		}
	}

	insertAt := trimTrailingBlank(lines, 0, boundary)
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:insertAt]...)
	result = append(result, field+" = "+encoded)
	result = append(result, lines[insertAt:]...)
	return result
}

// trimTrailingBlank returns end reduced past any blank lines immediately
// preceding it (down to start), so an insertion lands right after the last
// non-blank line of a section rather than after its trailing blank
// separator line.
func trimTrailingBlank(lines []string, start, end int) int {
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return end
}

// setTableKey replaces or inserts field inside [table], creating the table
// at the end of the file if it doesn't exist yet.
func setTableKey(lines []string, table, field, encoded string) []string {
	hdrRe := tableHeaderRe(table)
	start := -1
	for i, l := range lines {
		if hdrRe.MatchString(l) {
			start = i
			break
		}
	}

	if start == -1 {
		result := make([]string, len(lines), len(lines)+3)
		copy(result, lines)
		if len(result) > 0 {
			result = append(result, "")
		}
		result = append(result, "["+table+"]")
		result = append(result, field+" = "+encoded)
		return result
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if tomlHeaderLineRe.MatchString(lines[i]) {
			end = i
			break
		}
	}

	re := fieldLineRe(field)
	for i := start + 1; i < end; i++ {
		if m := re.FindStringSubmatch(lines[i]); m != nil {
			lines[i] = replaceFieldValue(m, field, encoded)
			return lines
		}
	}

	insertAt := trimTrailingBlank(lines, start+1, end)
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:insertAt]...)
	result = append(result, field+" = "+encoded)
	result = append(result, lines[insertAt:]...)
	return result
}

// replaceFieldValue rebuilds a matched "field = value  # comment" line with
// a new value, keeping the original indent, spacing around "=", and any
// trailing comment untouched.
func replaceFieldValue(m []string, field, encoded string) string {
	_, suffix := splitValueAndComment(m[3])
	return m[1] + field + m[2] + encoded + suffix
}

// splitValueAndComment splits the remainder of an assignment line (after
// "field =") into the value and a trailing suffix holding any whitespace
// and "# comment", tracking basic and literal TOML string quoting so a '#'
// inside a quoted value isn't mistaken for a comment. When there is no
// comment, the value has its trailing whitespace trimmed and the suffix is
// empty.
func splitValueAndComment(s string) (value, suffix string) {
	inDouble, inSingle, escaped := false, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inDouble {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inDouble = false
			}
			continue
		}
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		switch c {
		case '"':
			inDouble = true
		case '\'':
			inSingle = true
		case '#':
			j := i
			for j > 0 && (s[j-1] == ' ' || s[j-1] == '\t') {
				j--
			}
			return s[:j], s[j:]
		}
	}
	return strings.TrimRight(s, " \t"), ""
}

// dblQuotedReplacer mirrors BurntSushi/toml's basic-string escaping
// (encode.go's dblQuotedReplacer) so values SetKey writes are
// indistinguishable from what the library's own encoder would produce.
var dblQuotedReplacer = strings.NewReplacer(
	"\"", "\\\"",
	"\\", "\\\\",
	"\x00", `\u0000`,
	"\x01", `\u0001`,
	"\x02", `\u0002`,
	"\x03", `\u0003`,
	"\x04", `\u0004`,
	"\x05", `\u0005`,
	"\x06", `\u0006`,
	"\x07", `\u0007`,
	"\b", `\b`,
	"\t", `\t`,
	"\n", `\n`,
	"\x0b", `\u000b`,
	"\f", `\f`,
	"\r", `\r`,
	"\x0e", `\u000e`,
	"\x0f", `\u000f`,
	"\x10", `\u0010`,
	"\x11", `\u0011`,
	"\x12", `\u0012`,
	"\x13", `\u0013`,
	"\x14", `\u0014`,
	"\x15", `\u0015`,
	"\x16", `\u0016`,
	"\x17", `\u0017`,
	"\x18", `\u0018`,
	"\x19", `\u0019`,
	"\x1a", `\u001a`,
	"\x1b", `\u001b`,
	"\x1c", `\u001c`,
	"\x1d", `\u001d`,
	"\x1e", `\u001e`,
	"\x1f", `\u001f`,
	"\x7f", `\u007f`,
)

// encodeTOMLValue encodes value using BurntSushi/toml semantics: quoted
// and escaped for strings, and plain literals for bools and ints.
func encodeTOMLValue(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return `"` + dblQuotedReplacer.Replace(v) + `"`, nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.FormatInt(int64(v), 10), nil
	case int8:
		return strconv.FormatInt(int64(v), 10), nil
	case int16:
		return strconv.FormatInt(int64(v), 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	default:
		return "", fmt.Errorf("unsupported value type %T", value)
	}
}

// writeConfigFile writes content to path atomically: it encodes into a
// temp file created in the same directory, then renames it into place, so
// a crash or concurrent read never observes a partially written config
// file.
func writeConfigFile(path string, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".notty-config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp config file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once the rename below succeeds

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp config file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temp config file to %s: %w", path, err)
	}
	return nil
}
