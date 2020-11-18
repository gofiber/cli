package internal

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/containerd/console"
	"github.com/muesli/termenv"
)

var term = termenv.ColorProfile()

type finishedMsg struct{ error }

func checkConsole() (size console.WinSize, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()

	return console.Current().Size()
}

func errCmd(err error) tea.Cmd {
	return func() tea.Msg {
		return finishedMsg{err}
	}
}
