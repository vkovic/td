// Package tdtest holds the fixtures td's test packages share. It is imported
// only from _test.go files, so nothing in the td binary depends on it.
//
// It exists because the alternative is a fixture per package, and the copies
// drift: the git isolation below lived in five places, three of them
// byte-for-byte identical and two inlined into a harness, which is five places
// to forget when a new variable has to be neutralized.
package tdtest

import (
	"path/filepath"
	"testing"
)

// The identity td's tests commit under. It is a fixed name and an address in
// .invalid — the TLD reserved for exactly this — so a commit a test makes can
// never be mistaken for one a person made, and never depends on whatever the
// developer has in ~/.gitconfig.
const (
	AuthorName  = "td test"
	AuthorEmail = "td@example.invalid"
)

// gitEnv is the environment that keeps git off the developer's own
// configuration: a global and a system config file under configDir, neither of
// which exists, and a fixed author and committer.
//
// Both config paths have to be named. Pointing only GIT_CONFIG_GLOBAL at an
// empty file still leaves git reading /etc/gitconfig, where a commit.gpgsign or
// a core.hooksPath is enough to fail a test on one machine and pass on every
// other.
func gitEnv(configDir string) [][2]string {
	return [][2]string{
		{"GIT_CONFIG_GLOBAL", filepath.Join(configDir, "gitconfig")},
		{"GIT_CONFIG_SYSTEM", filepath.Join(configDir, "gitconfig-system")},
		{"GIT_AUTHOR_NAME", AuthorName},
		{"GIT_AUTHOR_EMAIL", AuthorEmail},
		{"GIT_COMMITTER_NAME", AuthorName},
		{"GIT_COMMITTER_EMAIL", AuthorEmail},
	}
}

// GitEnv is that environment as KEY=VALUE pairs, for a test that runs td as a
// child process and has to put them in exec.Cmd.Env. A test that reaches git in
// process wants IsolateGit instead.
func GitEnv(configDir string) []string {
	pairs := gitEnv(configDir)
	env := make([]string, 0, len(pairs))
	for _, p := range pairs {
		env = append(env, p[0]+"="+p[1])
	}
	return env
}

// IsolateGit applies that environment to the test process itself, for a test
// that reaches git in process through internal/gitx. The config files would go
// in a temp directory the test framework removes, if git ever created them.
func IsolateGit(t *testing.T) {
	t.Helper()
	for _, p := range gitEnv(t.TempDir()) {
		t.Setenv(p[0], p[1])
	}
}
