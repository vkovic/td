// Command td manages a markdown todo store under ~/.td.
package main

import (
	"fmt"
	"os"
)

// version is the binary's version string. Release builds override it with
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/td
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("td", version)
		os.Exit(0)
	}
	fmt.Println("td", version)
}
