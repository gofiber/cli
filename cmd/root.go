package cmd

import (
	"fmt"
	"os"

	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

const version = "0.0.2"

func init() {
	rootCmd.AddCommand(
		versionCmd, newCmd, DevCmd,
	)
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:               "fiber",
	Long:              longDescription,
	RunE:              rootRunE,
	PersistentPostRun: rootPersistentPostRun,
	SilenceErrors:     true,
}

var osExit = os.Exit

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		rootCmd.Println(err)
		osExit(1)
	}
}

func rootRunE(cmd *cobra.Command, _ []string) error {
	return cmd.Help()
}

func rootPersistentPostRun(cmd *cobra.Command, _ []string) {
	cliLatestVersion, err := latestVersion(true)
	if err != nil {
		return
	}

	if version != cliLatestVersion {
		warning := termenv.String(fmt.Sprintf(versionWarningFormat, version, cliLatestVersion)).
			Foreground(termenv.ANSIBrightYellow)
		cmd.Println(warning)
	}
}

const (
	longDescription = `🚀 Fiber is an Express inspired web framework written in Go with 💖
Learn more on https://gofiber.io

CLI version ` + version

	versionWarningFormat = `
WARNING: You are using fiber-cli version %s; however, version %s is available.
You should consider upgrading via the 'go get -u github.com/gofiber/fiber-cli' command.
`
)
