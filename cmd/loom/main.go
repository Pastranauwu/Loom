package main

import (
	"fmt"
	"os"
)

// version se inyecta en build con -ldflags "-X main.version=...";
// goreleaser lo hace por release. En un `go build` normal queda "dev".
var version = "dev"

func main() {
	root := newRootCmd()
	root.Version = version
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
