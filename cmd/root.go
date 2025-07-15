package cmd

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

const (
	configName     = ".fiberconfig"
	unknownVersion = "unknown"
)

var version string // dynamically determined version

// getVersion returns the current version using build info
// Falls back to VCS info if available, then to "unknown"
func getVersion() string {
	if version != "" {
		return version
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		version = unknownVersion
		return version
	}

	// First try to get version from module version (when built with go install)
	if buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
		version = strings.TrimPrefix(buildInfo.Main.Version, "v")
		return version
	}

	// Fallback to VCS info if available
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.tag":
			if setting.Value != "" {
				version = strings.TrimPrefix(setting.Value, "v")
				return version
			}
		case "vcs.revision":
			if setting.Value != "" && len(setting.Value) >= 7 {
				version = setting.Value[:7] // short commit hash
				return version
			}
		}
	}

	version = unknownVersion
	return version
}

var rc = rootConfig{
	CliVersionCheckInterval: int64((time.Hour * 12) / time.Second),
}

type rootConfig struct {
	CliVersionCheckInterval int64 `json:"cli_version_check_interval"`
	CliVersionCheckedAt     int64 `json:"cli_version_checked_at"`
}

func init() {
	// Set the long description dynamically with the current version
	rootCmd.Long = getLongDescription()

	rootCmd.AddCommand(
		versionCmd, newCmd, devCmd, upgradeCmd, migrateCmd,
	)
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:               "fiber",
	Long:              "", // will be set dynamically in init()
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
	return fmt.Errorf("help: %w", cmd.Help())
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

	cliLatestVersion, err := LatestCliVersion()
	if err != nil {
		return
	}

	if getVersion() != cliLatestVersion {
		title := termenv.String(fmt.Sprintf(versionUpgradeTitleFormat, getVersion(), cliLatestVersion)).
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
	if err := storeConfig(); err != nil {
		if _, pErr := fmt.Fprintf(os.Stdout, "failed to store config: %v\n", err); pErr != nil {
			fmt.Fprintf(os.Stderr, "print error: %v", pErr)
		}
	}
}

func needCheckCliVersion() bool {
	return !upgraded && rc.CliVersionCheckedAt+rc.CliVersionCheckInterval < time.Now().Unix()
}

// getLongDescription returns the long description with the current version
func getLongDescription() string {
	return `🚀 Fiber is an Express inspired web framework written in Go with 💖
Learn more on https://gofiber.io

CLI version ` + getVersion()
}

const (
	versionUpgradeTitleFormat = `
You are using fiber cli version %s; however, version %s is available.
Would you like to upgrade now? (y/N)`
)
