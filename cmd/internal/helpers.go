package internal

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/containerd/console"
	"github.com/muesli/termenv"
)

var term = termenv.ColorProfile()

type finishedMsg struct{ error }

func checkConsole() (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()

	console.Current()

	return
}

func errCmd(err error) tea.Cmd {
	return func() tea.Msg {
		return finishedMsg{err}
	}
}
