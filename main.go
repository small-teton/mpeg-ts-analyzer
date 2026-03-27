package main

import (
	_ "embed"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/cmd"
)

//go:embed VERSION
var version string

func main() {
	cmd.Execute(strings.TrimSpace(version))
}
