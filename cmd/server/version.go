package main

import (
	"fmt"
	"io"
)

var version = "dev"

func handleGlobalCLIArgs(args []string, stdout io.Writer) bool {
	if len(args) == 0 {
		return false
	}
	if stdout == nil {
		stdout = io.Discard
	}

	switch args[0] {
	case "help", "-h", "--help":
		writeUsage(stdout)
		return true
	case "version", "-v", "--version":
		fmt.Fprintln(stdout, version)
		return true
	default:
		return false
	}
}
