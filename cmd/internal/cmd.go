package internal

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

type SpinnerCmd struct {
	p            *tea.Program
	spinnerModel spinner.Model
	err          error
	title        string
	cmd          *exec.Cmd

	stdout chan []byte
	stderr chan []byte
	errCh  chan error
	buf    []byte
	done   bool
}

func NewSpinnerCmd(cmd *exec.Cmd, title ...string) *SpinnerCmd {
	spinnerModel := spinner.NewModel()
	spinnerModel.Spinner = spinner.Dot

	c := &SpinnerCmd{
		spinnerModel: spinnerModel,
		title:        "Running",
		cmd:          cmd,
		stdout:       make(chan []byte),
		stderr:       make(chan []byte),
		errCh:        make(chan error, 2),
	}

	if len(title) > 0 {
		c.title = title[0]
	}

	c.p = tea.NewProgram(c)

	return c
}

func (t *SpinnerCmd) Init() tea.Cmd {
	return tea.Batch(t.init(), spinner.Tick)
}

func (t *SpinnerCmd) init() tea.Cmd {
	return func() tea.Msg {
		if p, err := t.cmd.StdoutPipe(); err != nil {
			return finishedMsg{err}
		} else {
			go t.watchOutput(t.stdout, p)
		}
		if p, err := t.cmd.StderrPipe(); err != nil {
			return finishedMsg{err}
		} else {
			go t.watchOutput(t.stderr, p)
		}
		return finishedMsg{t.cmd.Start()}
	}
}

func (t *SpinnerCmd) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			t.err = fmt.Errorf("quit by %s\n", msg.String())
			return t, tea.Quit
		default:
			return t, nil
		}
	case finishedMsg:
		if t.err = msg.error; t.err != nil {
			return t, tea.Quit
		}
	}

	if t.done {
		return t, tea.Quit
	}

	if len(t.errCh) == 2 {
		close(t.errCh)
		if err := t.cmd.Wait(); err != nil {
			t.err = err
		}
		t.done = true
		t.title = "Finished"
		t.buf = nil
	}

	var cmd tea.Cmd
	t.spinnerModel, cmd = t.spinnerModel.Update(msg)
	return t, cmd
}

func (t *SpinnerCmd) View() string {
	if t.err != nil {
		return ""
	}

	s := termenv.
		String(t.spinnerModel.View()).
		Foreground(term.Color("205")).
		String()

	t.UpdateOutput(t.stdout)
	t.UpdateOutput(t.stderr)

	return fmt.Sprintf(spinnerCmdTemplate, s, t.title, t.buf)
}

const spinnerCmdTemplate = `
  %s %s %s

     (esc/q/ctrl+c to quit)
    
`

func (t *SpinnerCmd) Run() (err error) {
	if err = checkConsole(); err != nil {
		return
	}

	if err = t.p.Start(); err != nil {
		return
	}

	return t.err
}

func (t *SpinnerCmd) UpdateOutput(c <-chan []byte) {
	select {
	case b := <-c:
		if !bytes.Equal(t.buf, b) {
			t.buf = b
		}
	default:
	}
}

func (t *SpinnerCmd) watchOutput(out chan<- []byte, rc io.ReadCloser) {
	defer func() { _ = rc.Close() }()
	br := bufio.NewReader(rc)
	for {
		b, _, err := br.ReadLine()
		if err != nil {
			t.errCh <- err
			return
		}
		out <- b
	}
}
