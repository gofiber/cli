package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:  "fiber",
	Long: "🚀 Fiber is an Express inspired web framework written in Go with 💖\n Learn more on https://gofiber.io",
}

var osExit = os.Exit

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		osExit(1)
	}
}

// FiberCmd indicates fiber-cli's root command
func FiberCmd() *cobra.Command {
	return rootCmd
}
