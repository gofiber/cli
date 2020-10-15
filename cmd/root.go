package cmd

import (
	"fmt"
	"os"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
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

	const (
		// cBlack = "\u001b[90m"
		// cRed   = "\u001b[91m"
		// cCyan = "\u001b[96m"
		// cGreen = "\u001b[92m"
		cYellow = "\u001b[93m"
		// cBlue    = "\u001b[94m"
		// cMagenta = "\u001b[95m"
		// cWhite   = "\u001b[97m"
		cReset = "\u001b[0m"
	)

	out := colorable.NewColorableStdout()
	if os.Getenv("TERM") == "dumb" || (!isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd())) {
		out = colorable.NewNonColorable(os.Stdout)
	}

	if cliVersion != cliLatestVersion {
		fmt.Fprintf(out, "\n%sWARNING: You are using fiber-cli version %s; however, version %s is available.\n", cYellow, cliVersion, cliLatestVersion)
		fmt.Fprintf(out, "You should consider upgrading via the 'go get -u github.com/gofiber/fiber-cli' command.%s", cReset)
	}
}
