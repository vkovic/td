package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig drops a config.toml in a temp store root and returns the root.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// clearEnv unsets every variable that could leak in from the developer's own
// shell, so a test asserts on td's own resolution and nothing else.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{EnvDoneTTLDays, EnvAutoCommit, EnvAutoPush, EnvEditor, "EDITOR"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

func TestMissingFileYieldsDefaults(t *testing.T) {
	clearEnv(t)
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Default()
	if got.DoneTTLDays != want.DoneTTLDays || got.AutoCommit != want.AutoCommit || got.AutoPush != want.AutoPush {
		t.Errorf("Load with no config file = %+v, want the defaults %+v", got, want)
	}
	if got.Editor != fallbackEditor {
		t.Errorf("Editor = %q, want the %s fallback", got.Editor, fallbackEditor)
	}
}

func TestDefaults(t *testing.T) {
	d := Default()
	if d.DoneTTLDays != 7 {
		t.Errorf("DoneTTLDays default = %d, want 7", d.DoneTTLDays)
	}
	if !d.AutoCommit || !d.AutoPush {
		t.Errorf("auto_commit/auto_push defaults = %v/%v, want true/true", d.AutoCommit, d.AutoPush)
	}
}

func TestPartialFileFillsRestFromDefaults(t *testing.T) {
	clearEnv(t)
	root := writeConfig(t, "done_ttl_days = 30\n")
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DoneTTLDays != 30 {
		t.Errorf("DoneTTLDays = %d, want 30 from the file", got.DoneTTLDays)
	}
	if !got.AutoCommit || !got.AutoPush {
		t.Errorf("unset keys = %v/%v, want the defaults true/true", got.AutoCommit, got.AutoPush)
	}
}

func TestFileSetsEveryKey(t *testing.T) {
	clearEnv(t)
	root := writeConfig(t, `
done_ttl_days = 0
auto_commit = false
auto_push = false
editor = "code --wait"
`)
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DoneTTLDays != 0 {
		t.Errorf("DoneTTLDays = %d, want 0", got.DoneTTLDays)
	}
	if got.AutoCommit || got.AutoPush {
		t.Errorf("auto_commit/auto_push = %v/%v, want false/false", got.AutoCommit, got.AutoPush)
	}
	if got.Editor != "code --wait" {
		t.Errorf("Editor = %q, want %q", got.Editor, "code --wait")
	}
}

func TestFalseInFileIsNotTreatedAsUnset(t *testing.T) {
	clearEnv(t)
	root := writeConfig(t, "auto_push = false\n")
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AutoPush {
		t.Error("auto_push = true; a key written as false must not fall back to the default")
	}
}

func TestEnvBeatsFileAndDefaults(t *testing.T) {
	clearEnv(t)
	root := writeConfig(t, "done_ttl_days = 30\nauto_commit = true\nauto_push = true\neditor = \"vim\"\n")
	t.Setenv(EnvDoneTTLDays, "2")
	t.Setenv(EnvAutoCommit, "false")
	t.Setenv(EnvAutoPush, "0")
	t.Setenv(EnvEditor, "nano")

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DoneTTLDays != 2 {
		t.Errorf("DoneTTLDays = %d, want 2 from the environment", got.DoneTTLDays)
	}
	if got.AutoCommit {
		t.Error("auto_commit = true, want the environment's false")
	}
	if got.AutoPush {
		t.Error("auto_push = true, want the environment's 0")
	}
	if got.Editor != "nano" {
		t.Errorf("Editor = %q, want nano from the environment", got.Editor)
	}
}

func TestEnvBoolForms(t *testing.T) {
	clearEnv(t)
	root := t.TempDir()
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"true", true}, {"TRUE", true}, {"1", true}, {"t", true},
		{"false", false}, {"FALSE", false}, {"0", false}, {"f", false},
		{"  false  ", false},
	} {
		t.Setenv(EnvAutoPush, tc.in)
		got, err := Load(root)
		if err != nil {
			t.Fatalf("Load with %s=%q: %v", EnvAutoPush, tc.in, err)
		}
		if got.AutoPush != tc.want {
			t.Errorf("%s=%q gave AutoPush=%v, want %v", EnvAutoPush, tc.in, got.AutoPush, tc.want)
		}
	}
}

func TestEmptyEnvIsTreatedAsUnset(t *testing.T) {
	clearEnv(t)
	root := writeConfig(t, "auto_push = false\n")
	t.Setenv(EnvAutoPush, "")
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AutoPush {
		t.Error("an empty TD_AUTO_PUSH overrode the file, want it treated as unset")
	}
}

func TestMalformedValuesError(t *testing.T) {
	clearEnv(t)
	root := t.TempDir()

	envCases := []struct{ name, value string }{
		{EnvDoneTTLDays, "soon"},
		{EnvDoneTTLDays, "7.5"},
		{EnvDoneTTLDays, "-1"},
		{EnvAutoCommit, "yes please"},
		{EnvAutoPush, "maybe"},
	}
	for _, tc := range envCases {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tc.name, tc.value)
			if _, err := Load(root); err == nil {
				t.Errorf("Load with %s=%q = nil error, want a failure rather than a silent default", tc.name, tc.value)
			}
		})
	}

	fileCases := []struct{ name, body string }{
		{"ttl is a string", `done_ttl_days = "seven"`},
		{"ttl is negative", `done_ttl_days = -1`},
		{"bool is a string", `auto_commit = "true"`},
		{"editor is a number", `editor = 3`},
		{"not toml at all", "this is not = = toml"},
	}
	for _, tc := range fileCases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			if _, err := Load(writeConfig(t, tc.body+"\n")); err == nil {
				t.Errorf("Load with %q = nil error, want a failure", tc.body)
			}
		})
	}
}

func TestErrorNamesTheSource(t *testing.T) {
	clearEnv(t)
	root := writeConfig(t, "done_ttl_days = -3\n")
	_, err := Load(root)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "done_ttl_days") {
		t.Errorf("error %v does not name the offending key", err)
	}

	clearEnv(t)
	t.Setenv(EnvAutoPush, "maybe")
	_, err = Load(t.TempDir())
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), EnvAutoPush) {
		t.Errorf("error %v does not name the offending variable", err)
	}
}

func TestUnknownKeysReported(t *testing.T) {
	clearEnv(t)
	root := writeConfig(t, "done_ttl_days = 7\ndone_ttl_dyas = 9\ncolour = \"blue\"\n")
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DoneTTLDays != 7 {
		t.Errorf("DoneTTLDays = %d, want the recognized key's 7", got.DoneTTLDays)
	}
	if len(got.UnknownKeys) != 2 {
		t.Fatalf("UnknownKeys = %v, want the two unrecognized keys", got.UnknownKeys)
	}
	joined := strings.Join(got.UnknownKeys, " ")
	for _, want := range []string{"done_ttl_dyas", "colour"} {
		if !strings.Contains(joined, want) {
			t.Errorf("UnknownKeys %v does not name %s", got.UnknownKeys, want)
		}
	}
}

func TestEditorFallsBackThroughEnv(t *testing.T) {
	clearEnv(t)
	root := t.TempDir()

	// Nothing set at all.
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Editor != fallbackEditor {
		t.Errorf("Editor = %q, want %q", got.Editor, fallbackEditor)
	}

	// $EDITOR fills in when neither the config nor TD_EDITOR names one.
	t.Setenv("EDITOR", "helix")
	got, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Editor != "helix" {
		t.Errorf("Editor = %q, want helix from $EDITOR", got.Editor)
	}

	// The config file beats $EDITOR.
	root = writeConfig(t, `editor = "vim"`+"\n")
	got, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Editor != "vim" {
		t.Errorf("Editor = %q, want vim from the config file", got.Editor)
	}

	// TD_EDITOR beats both.
	t.Setenv(EnvEditor, "nano")
	got, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Editor != "nano" {
		t.Errorf("Editor = %q, want nano from TD_EDITOR", got.Editor)
	}
}
