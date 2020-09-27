package cmd

import (
	"errors"
	"fmt"
	"os"

	"fiber-cli/pkg/fibercli"
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

		var tType fibercli.TemplateType

		switch templateType {
		case fibercli.TemplateBasic.String():
			tType = fibercli.TemplateBasic
			break
		case fibercli.TemplateComplex.String():
			tType = fibercli.TemplateComplex
			break
		default:
			return errors.New("invalid template type")
		}

		newProj := fibercli.New{
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

	newCmd.Flags().StringVarP(&templateType, "type", "t", "basic", "basic | complex")
}
