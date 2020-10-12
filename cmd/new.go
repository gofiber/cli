package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"text/template"

	"github.com/spf13/cobra"
)

var templateType string

// newCmd represents the new command
var newCmd = &cobra.Command{
	Use:   "new [projectName]",
	Short: "Generates boilerplate",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 || args[0] == "" {
			return errors.New("project name not specify")
		}

		projectName := args[0]
		wd, err := os.Getwd()
		if err != nil {
			return err
		}

		var tType TemplateType

		switch templateType {
		case TemplateBasic.String():
			tType = TemplateBasic
			break
		case TemplateComplex.String():
			tType = TemplateComplex
			break
		default:
			return errors.New("invalid template type")
		}

		newProj := New{
			Name: projectName,
			Path: wd,
			Type: tType,
		}
		if err := newProj.Create(); err != nil {
			return err
		}

		fmt.Printf("Created %s project\n", projectName)
		fmt.Println("Get started by running")
		fmt.Printf("cd %s && go run main.go ", projectName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(newCmd)

	newCmd.Flags().StringVarP(&templateType, "template", "t", "basic", "basic|complex")
}

type TemplateType int

const (
	TemplateBasic TemplateType = iota
	TemplateComplex
)

func (t TemplateType) String() string {
	return [...]string{"basic", "complex"}[t]
}

type New struct {
	Name string
	Path string
	Type TemplateType
}

// Creates new project based on template
func (n New) Create() error {
	if n.Type == TemplateBasic {
		return createBasic(n.Path, n.Name)
	}
	return createComplex(n.Path, n.Name)
}

// Create basic project from go template
func createBasic(dir, projectName string) error {
	path := fmt.Sprintf("%s/%s", dir, projectName)
	if err := os.Mkdir(path, os.ModePerm); err != nil {
		return err
	}

	if err := os.Chdir(path); err != nil {
		return err
	}

	// create main.go
	mainFile, err := os.Create(fmt.Sprintf("%s/main.go", path))
	if err != nil {
		return err
	}

	defer func() {
		if err := mainFile.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	mainTemplate := template.Must(template.New("main").Parse(string(MainTemplate())))
	if err := mainTemplate.Execute(mainFile, nil); err != nil {
		return err
	}

	cmdInit := exec.Command("go", "mod", "init", projectName)
	if err := cmdInit.Run(); err != nil {
		return err
	}

	cmdTidy := exec.Command("go", "mod", "tidy")
	if err := cmdTidy.Run(); err != nil {
		return err
	}

	return nil
}

// create project from boilerplate repository
func createComplex(dir, projectName string) error {
	path := fmt.Sprintf("%s/%s", dir, projectName)
	if err := os.Mkdir(path, os.ModePerm); err != nil {
		return err
	}

	if err := os.Chdir(path); err != nil {
		return err
	}

	if err := Clone(path, BoilerPlateRepo); err != nil {
		return err
	}

	if err := Replace(path, "go.mod", "boilerplate", projectName); err != nil {
		return err
	}

	if err := Replace(path, "*.go", "boilerplate", projectName); err != nil {
		return err
	}

	return nil
}
