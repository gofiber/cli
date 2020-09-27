package fibercli

import (
	"fmt"
	"os"
	"os/exec"
	"text/template"
)

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

func (n New) Create() error {
	if n.Type == TemplateBasic {
		return createBasic(n.Path, n.Name)
	}
	return createComplex(n.Path, n.Name)
}

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

	if err := ReplaceFiles(path, "go.mod", "boilerplate", projectName); err != nil {
		return err
	}

	if err := ReplaceFiles(path, "*.go", "boilerplate", projectName); err != nil {
		return err
	}

	return nil
}
