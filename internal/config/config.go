// Package config reads td's settings: the defaults, what ~/.td/config.toml
// overrides, and what the environment overrides on top of that.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// FileName is the config file's name inside the store root.
const FileName = "config.toml"

// The environment variables that override a config key for one run.
const (
	EnvDoneTTLDays = "TD_DONE_TTL_DAYS"
	EnvAutoCommit  = "TD_AUTO_COMMIT"
	EnvAutoPush    = "TD_AUTO_PUSH"
	EnvEditor      = "TD_EDITOR"
)

// fallbackEditor is used when neither the config, TD_EDITOR, nor EDITOR names
// one — vi is the editor POSIX guarantees is present.
const fallbackEditor = "vi"

// Config is td's resolved settings.
type Config struct {
	// DoneTTLDays is how long a done item stays in the list before the archive
	// sweep moves it to archived/. Zero sweeps it on the next command.
	DoneTTLDays int
	// AutoCommit commits every change to the store's git repository.
	AutoCommit bool
	// AutoPush pushes after a change, when the repository has a remote.
	AutoPush bool
	// Editor is the command used to open an item's body.
	Editor string

	// UnknownKeys are keys found in the config file that td does not recognize.
	// They are almost always typos, so callers report them rather than letting
	// a misspelled key silently do nothing.
	UnknownKeys []string
}

// Default returns the settings that apply when nothing overrides them.
func Default() Config {
	return Config{
		DoneTTLDays: 7,
		AutoCommit:  true,
		AutoPush:    true,
	}
}

// file mirrors the config file's keys as pointers, so a key that is absent is
// distinguishable from one written with its zero value.
type file struct {
	DoneTTLDays *int    `toml:"done_ttl_days"`
	AutoCommit  *bool   `toml:"auto_commit"`
	AutoPush    *bool   `toml:"auto_push"`
	Editor      *string `toml:"editor"`
}

// Load reads the settings for the store at root: the defaults, overridden by
// ~/.td/config.toml where it sets a key, overridden in turn by the TD_
// environment variables. A missing config file is not an error.
func Load(root string) (Config, error) {
	cfg, err := loadFile(filepath.Join(root, FileName))
	if err != nil {
		return Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	cfg.Editor = resolveEditor(cfg.Editor)
	return cfg, nil
}

// loadFile applies a config file to the defaults. A file that is absent leaves
// the defaults untouched; one that will not parse, or that holds a value of the
// wrong type, is an error rather than a silent fallback.
func loadFile(path string) (Config, error) {
	cfg := Default()

	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var f file
	md, err := toml.Decode(string(b), &f)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	for _, key := range md.Undecoded() {
		cfg.UnknownKeys = append(cfg.UnknownKeys, key.String())
	}

	if f.DoneTTLDays != nil {
		if err := setTTL(&cfg, *f.DoneTTLDays, path+": done_ttl_days"); err != nil {
			return Config{}, err
		}
	}
	if f.AutoCommit != nil {
		cfg.AutoCommit = *f.AutoCommit
	}
	if f.AutoPush != nil {
		cfg.AutoPush = *f.AutoPush
	}
	if f.Editor != nil {
		cfg.Editor = *f.Editor
	}
	return cfg, nil
}

// applyEnv lets the TD_ variables override whatever the file and defaults set.
// A variable that is present but unreadable is an error: a typo in TD_AUTO_PUSH
// must not quietly leave pushing on.
func applyEnv(cfg *Config) error {
	if v, ok := lookupEnv(EnvDoneTTLDays); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s=%q: want a whole number of days", EnvDoneTTLDays, v)
		}
		if err := setTTL(cfg, n, EnvDoneTTLDays); err != nil {
			return err
		}
	}
	for _, e := range []struct {
		name string
		dst  *bool
	}{
		{EnvAutoCommit, &cfg.AutoCommit},
		{EnvAutoPush, &cfg.AutoPush},
	} {
		v, ok := lookupEnv(e.name)
		if !ok {
			continue
		}
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("%s=%q: want true or false", e.name, v)
		}
		*e.dst = b
	}
	if v, ok := lookupEnv(EnvEditor); ok {
		cfg.Editor = v
	}
	return nil
}

// setTTL validates and stores a done TTL. A negative TTL has no meaning and is
// more likely a typo than an intent to never archive.
func setTTL(cfg *Config, days int, source string) error {
	if days < 0 {
		return fmt.Errorf("%s: %d is negative; want zero or more days", source, days)
	}
	cfg.DoneTTLDays = days
	return nil
}

// resolveEditor falls back from the configured editor to $EDITOR and finally to
// vi, so opening an item's body always has something to run.
func resolveEditor(configured string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	if v := strings.TrimSpace(os.Getenv("EDITOR")); v != "" {
		return v
	}
	return fallbackEditor
}

// lookupEnv reads a variable, treating one set to the empty string as unset so
// `TD_AUTO_PUSH= td ls` does not fail on an unparsable empty value.
func lookupEnv(name string) (string, bool) {
	v, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(v) == "" {
		return "", false
	}
	return v, true
}
