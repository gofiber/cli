package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(
		versionCmd, newCmd, DevCmd,
	)
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:           "fiber",
	SilenceErrors: true,
}

var osExit = os.Exit

var cliVersion string
var cliLatestVersion string
// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(version string) {
	cliVersion = version

	versionString := "CLI version " + cliVersion
	var err error
	if cliLatestVersion, err = latestVersion(true); err == nil {
		versionString += fmt.Sprintf(" (latest %s)", cliLatestVersion)
	} else {
		cliLatestVersion = cliVersion
	}

	rootCmd.Long = fmt.Sprintf("🚀 Fiber is an Express inspired web framework written in Go with 💖\n Learn more on https://gofiber.io\n\n%s", versionString)

	if err := rootCmd.Execute(); err != nil {
		rootCmd.Println(err)
		osExit(1)
	}
}
