package main

import (
	"fiber-cli/cmd"
	"log"

	"github.com/spf13/cobra/doc"
)

func main() {
	err := doc.GenMarkdownTree(cmd.FiberCmd(), "./docs")
	if err != nil {
		log.Fatal(err)
	}
}
