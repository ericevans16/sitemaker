package main

import (
	"os"

	"github.com/ericevans16/sitemaker/internal/cli"
)

var version = "0.1.0"

func main() {
	os.Exit(cli.Execute(os.Args[1:], version))
}
