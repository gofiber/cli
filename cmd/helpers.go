package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
)

func Exist(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}
	return true
}

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

// Replaces matching file patterns in a path, including subdirectories
func Replace(path, pattern, old, new string) error {
	err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		return replaceWalkFn(path, info, err, pattern, old, new)
	})

	if err != nil {
		return err
	}
	return nil
}

func replaceWalkFn(path string, fi os.FileInfo, err error, pattern, old, new string) error {

	if err != nil {
		return err
	}

	if !!fi.IsDir() {
		return nil
	}

	matched, err := filepath.Match(pattern, fi.Name())

	if err != nil {
		return err
	}

	if matched {
		read, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}

		newContents := strings.Replace(string(read), old, new, -1)

		err = ioutil.WriteFile(path, []byte(newContents), 0)
		if err != nil {
			return err
		}
	}

	return nil
}

func MainTemplate() []byte {
	return []byte(`package main

import (
	"github.com/gofiber/fiber/v2"
	"log"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	app := fiber.New()

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Hello, World!")
	})
	
	return app.Listen(":3000")
}
`)
}
