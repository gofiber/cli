package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var templateType string

func init() {
	newCmd.Flags().StringVarP(&templateType, "template", "t", "basic", "basic|complex")
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

	wd, _ := os.Getwd()
	projectPath := fmt.Sprintf("%s%c%s", wd, os.PathSeparator, projectName)

	if err = createProject(projectPath); err != nil {
		return
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(projectPath)
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

func createProject(projectPath string) (err error) {
	if err = os.Mkdir(projectPath, 0750); err != nil {
		return
	}

	return os.Chdir(projectPath)
}

func createBasic(projectPath, modName string) (err error) {
	// create main.go
	if err = createFile(fmt.Sprintf("%s%cmain.go", projectPath, os.PathSeparator), newBasicTemplate); err != nil {
		return
	}

	return runCmd("go", "mod", "init", modName)
}

const boilerPlateRepo = "https://github.com/gofiber/boilerplate"

func createComplex(projectPath, modName string) (err error) {
	var git string
	if git, err = execLookPath("git"); err != nil {
		return
	}

	if err = runCmd(git, "clone", boilerPlateRepo, projectPath); err != nil {
		return
	}

	if err = replace(projectPath, "go.mod", "boilerplate", modName); err != nil {
		return
	}

	if err = replace(projectPath, "*.go", "boilerplate", modName); err != nil {
		return
	}

	return
}

var (
	newExamples = `  fiber new fiber-demo
    Generates a project with go module name fiber-demo

  fiber new fiber-demo your.own/module/name
    Specific the go module name

  fiber new fiber-demo -t=complex
    Generate a complex project`

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
