package main

import (
	"os"

	"loom/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
