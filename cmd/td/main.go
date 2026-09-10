// Command td manages a markdown todo store under ~/.td.
package main

import (
	"os"
	"runtime/debug"
	"sync"
)

// version is the binary's version string. A release build overrides it with
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/td
//
// and every other build leaves it at devVersion for buildVersion to resolve.
var version = "dev"

// devVersion is the placeholder a build that passed no ldflags carries.
const devVersion = "dev"

func main() {
	os.Exit(Execute())
}

// resolvedVersion is what --version prints. It is computed once, on the first
// command that asks, rather than in main, so a td built as a library test sees
// the same answer the binary does.
var resolvedVersion = sync.OnceValue(func() string {
	return buildVersion(version, debug.ReadBuildInfo)
})

// buildVersion resolves what td --version prints.
//
// An ldflags version wins outright. Otherwise the answer comes from the build
// information the toolchain stamps in anyway, and which nothing used to read:
// the commit for a binary built from a checkout, marked -dirty when the tree
// had uncommitted changes, and the module version for one installed with go
// install. Printing "dev" for every one of those, as td did, tells a bug
// report nothing about which td it came from.
func buildVersion(ldflags string, read func() (*debug.BuildInfo, bool)) string {
	if ldflags != devVersion {
		return ldflags
	}
	info, ok := read()
	if !ok {
		return devVersion
	}

	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	// A checkout build is named by its commit. Its Main.Version is a
	// synthesized pseudo-version — v0.0.0-20260910152159-7c64f17834d2+dirty —
	// which carries the same commit inside forty characters of noise.
	if revision != "" {
		if len(revision) > shortRevision {
			revision = revision[:shortRevision]
		}
		if modified == "true" {
			return revision + "-dirty"
		}
		return revision
	}

	// No VCS information means the build came from the module cache, which is
	// what go install github.com/vkovic/td/cmd/td@v0.1.0 does. There the
	// module version is the tag, and it is the best name the binary has.
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return devVersion
}

// shortRevision is how much of the commit hash td prints: long enough to be
// unambiguous, short enough to read back off a terminal.
const shortRevision = 12
