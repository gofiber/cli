package v3

import (
	"testing"
)

func Test_isFiberApp(t *testing.T) {
	src := `package main
import "github.com/gofiber/fiber/v2"
func main() {
        app := fiber.New()
        _ = app
}`
	if !isFiberApp(src, "app") {
		t.Fatal("expected app to be recognized")
	}
	if isFiberApp(src, "main") {
		t.Fatal("unexpected match")
	}
}

func Test_isFiberRouter(t *testing.T) {
	src := `package main
import "github.com/gofiber/fiber/v2"
func main() {
        app := fiber.New()
        grp := app.Group("/api")
        var r fiber.Router
        r = app
        _ = grp
        _ = r
}`
	if !isFiberRouter(src, "app") || !isFiberRouter(src, "grp") || !isFiberRouter(src, "r") {
		t.Fatal("router identifiers not detected")
	}
	if isFiberRouter(src, "main") {
		t.Fatal("unexpected match")
	}
}

func Test_isFiberCtx(t *testing.T) {
	src := `package main
import "github.com/gofiber/fiber/v2"
func handler(c fiber.Ctx) error { return nil }`
	if !isFiberCtx(src, "c") {
		t.Fatal("expected context to be recognized")
	}
	if isFiberCtx(src, "handler") {
		t.Fatal("unexpected match")
	}
}
