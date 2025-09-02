package cmd

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/spf13/cobra"
)

var (
	templateType string
	repo         string
)

func init() {
	newCmd.Flags().StringVarP(&templateType, "template", "t", "basic", "basic|complex")
	newCmd.Flags().StringVarP(&repo, "repo", "r", defaultRepo, "complex boilerplate repo name in github or other repo url")
}

var newCmd = &cobra.Command{
	Use:     "new PROJECT [module name]",
	Aliases: []string{"n"},
	Short:   "Generate a new fiber project",
	Example: newExamples,
	Args:    cobra.MinimumNArgs(1),
	RunE:    newRunE,
}

func newRunE(cmd *cobra.Command, args []string) (err error) {
	start := time.Now()

	projectName := args[0]
	modName := projectName
	if len(args) > 1 {
		modName = args[1]
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	projectPath := fmt.Sprintf("%s%c%s", wd, os.PathSeparator, projectName)

	if err := createProject(projectPath); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if rmErr := os.RemoveAll(projectPath); rmErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "failed to remove project dir: %v", rmErr)
			}
		}
	}()

	create := createBasic
	if templateType != "basic" {
		create = createComplex
	}

	defer func() {
		if err == nil {
			cmd.Printf(newSuccessTemplate,
				projectPath, modName, projectName, formatLatency(time.Since(start)))
		}
	}()

	return create(projectPath, modName)
}

func createProject(projectPath string) error {
	if err := os.Mkdir(projectPath, 0o750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if err := os.Chdir(projectPath); err != nil {
		return fmt.Errorf("change directory: %w", err)
	}

	return nil
}

func createBasic(projectPath, modName string) error {
	if err := createFile(fmt.Sprintf("%s%cmain.go", projectPath, os.PathSeparator), newBasicTemplate); err != nil {
		return err
	}

	if err = runCmd(execCommand("go", "mod", "init", modName)); err != nil{
		return
	}
	
	//Execute go mod tidy in the project directory
	installModules := execCommand("go", "mod", "tidy")
	installModules.Dir = fmt.Sprintf("%s%c", projectPath, os.PathSeparator)
	if err = runCmd(installModules); err != nil{
		return
	}

	return
}

const (
	githubPrefix = "https://github.com/"
	defaultRepo  = "gofiber/boilerplate"
)

var fullPathRegex = regexp.MustCompile(`^(http|https|git)`)

func createComplex(projectPath, modName string) error {
	git, err := execLookPath("git")
	if err != nil {
		return err
	}

	toClone := githubPrefix + repo
	if isFullPath := fullPathRegex.MatchString(repo); isFullPath {
		toClone = repo
	}

	if err := runCmd(execCommand(git, "clone", toClone, projectPath)); err != nil {
		return err
	}

	if repo == defaultRepo {
		if err := replace(projectPath, "go.mod", "boilerplate", modName); err != nil {
			return err
		}

		if err := replace(projectPath, "*.go", "boilerplate", modName); err != nil {
			return err
		}
	}
	return nil
}

var (
	newExamples = `  fiber new fiber-demo
  Generates a project with go module name fiber-demo

  fiber new fiber-demo your.own/module/name
  Specific the go module name

  fiber new fiber-demo -t=complex
  Generate a complex project

  fiber new fiber-demo -t complex -r githubId/repo
  Generate project based on Github repo

  fiber new fiber-demo -t complex -r https://anyProvider.com/username/repo.git
  Generate project based on repo outside Github with https

  fiber new fiber-demo -t complex -r git@anyProvider.com:id/repo.git
  Generate project based on repo outside Github with ssh`

	newBasicTemplate = `package main

import (
    "log"

    "github.com/gofiber/fiber/v2"
)

func main() {
    app := fiber.New()

    app.Get("/", func(c *fiber.Ctx) error {
        return c.SendString("Hello, World!")
    })

    log.Fatal(app.Listen(":3000"))
}`

	newSuccessTemplate = `
Scaffolding project in %s (module %s)

  Done. Now run:

  cd %s
  fiber dev

✨  Done in %s.
`
)
