// Command td manages a markdown todo store under ~/.td.
package main

import "os"

// version is the binary's version string. Release builds override it with
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/td
var version = "dev"

func main() {
	os.Exit(Execute())
}
