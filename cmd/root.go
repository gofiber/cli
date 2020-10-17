package cmd

import (
	"fmt"
	"time"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

const version = "0.0.2"
const configName = ".fiberconfig"

var (
	rc = rootConfig{
		CliVersionCheckInterval: int64((time.Hour * 12) / time.Second),
	}
)

type rootConfig struct {
	CliVersionCheckInterval int64 `json:"cli_version_check_interval"`
	CliVersionCheckedAt     int64 `json:"cli_version_checked_at"`
}

func init() {
	rootCmd.AddCommand(
		versionCmd, newCmd, devCmd, upgradeCmd,
	)
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:               "fiber",
	Long:              longDescription,
	RunE:              rootRunE,
	PersistentPreRun:  rootPersistentPreRun,
	PersistentPostRun: rootPersistentPostRun,
	SilenceErrors:     true,
}

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

func rootPersistentPreRun(cmd *cobra.Command, _ []string) {
	if err := loadConfig(); err != nil {
		warning := fmt.Sprintf("WARNING: failed to load config: %s\n\n", err)
		cmd.Println(termenv.String(warning).Foreground(termenv.ANSIBrightYellow))
	}
}

func rootPersistentPostRun(cmd *cobra.Command, _ []string) {
	checkCliVersion(cmd)
}

func checkCliVersion(cmd *cobra.Command) {
	if !needCheckCliVersion() {
		return
	}

	cliLatestVersion, err := latestVersion(true)
	if err != nil {
		return
	}

	if version != cliLatestVersion {
		title := termenv.String(fmt.Sprintf(versionUpgradeTitleFormat, version, cliLatestVersion)).
			Foreground(termenv.ANSIBrightYellow)

		prompt := internal.NewPrompt(title.String())
		ok, err := prompt.YesOrNo()

		if err == nil && ok {
			upgrade(cmd, cliLatestVersion)
		}

		if err != nil {
			warning := fmt.Sprintf("WARNING: Failed to upgrade fiber cli: %s", err)
			cmd.Println(termenv.String(warning).Foreground(termenv.ANSIBrightYellow))
		}
	}

	updateVersionCheckedAt()
}

func updateVersionCheckedAt() {
	rc.CliVersionCheckedAt = time.Now().Unix()
	storeConfig()
}

func needCheckCliVersion() bool {
	return rc.CliVersionCheckedAt+rc.CliVersionCheckInterval < time.Now().Unix()
}

const (
	longDescription = `🚀 Fiber is an Express inspired web framework written in Go with 💖
Learn more on https://gofiber.io

CLI version ` + version

	versionUpgradeTitleFormat = `
You are using fiber cli version %s; however, version %s is available.
Would you like to upgrade now? (y/N)`
)
