package fibercli

import (
	"fmt"
	"github.com/go-git/go-git/v5"
)

const BoilerPlateRepo = "https://github.com/gofiber/boilerplate"

// Git clone repository
func Clone(path, repo string) error {
	_, err := git.PlainClone(path, false, &git.CloneOptions{
		URL: repo,
	})
	if err != nil {
		return fmt.Errorf("error in cloning repository %v: %v", repo, err)
	}

	return nil
}
