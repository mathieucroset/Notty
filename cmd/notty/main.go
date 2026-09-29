// Command notty is a terminal note-taking app with GitHub sync.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("notty " + version)
		return
	}

	fmt.Fprintln(os.Stderr, "notty: TUI not implemented yet")
	os.Exit(1)
}
