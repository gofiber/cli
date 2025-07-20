package cmd

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func Test_ServeRunE(t *testing.T) {
	old := listen
	listen = func(_ *fiber.App, _ string, _ fiber.ListenConfig) error { return nil }
	defer func() { listen = old }()

	_, err := runCobraCmd(serveCmd, "--dir=.")
	require.NoError(t, err)
}

func Test_ServeRunE_Error(t *testing.T) {
	old := listen
	listen = func(_ *fiber.App, _ string, _ fiber.ListenConfig) error { return errors.New("fail") }
	defer func() { listen = old }()

	_, err := runCobraCmd(serveCmd, "--dir=.")
	require.Error(t, err)
}
