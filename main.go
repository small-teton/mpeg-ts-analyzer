package main

import (
	_ "embed"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/cmd"
)

//go:embed VERSION
var version string

func main() {
	cmd.Execute(strings.TrimSpace(version))
}
