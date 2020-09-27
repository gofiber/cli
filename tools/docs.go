package main

import (
	"fiber-cli/cmd"
	"github.com/spf13/cobra/doc"
	"log"
)

func main() {
	err := doc.GenMarkdownTree(cmd.FiberCmd(), "./")
	if err != nil {
		log.Fatal(err)
	}
}
