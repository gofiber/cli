package internal

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/containerd/console"
	"github.com/muesli/termenv"
)

type SpinnerCmd struct {
	err error
	p   *tea.Program
	cmd *exec.Cmd

	stdout       chan []byte
	stderr       chan []byte
	errCh        chan error
	title        string
	buf          []byte
	spinnerModel spinner.Model
	size         console.WinSize
	done         bool
}

func NewSpinnerCmd(cmd *exec.Cmd, title ...string) *SpinnerCmd {
	spinnerModel := spinner.New()
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
	return tea.Batch(t.start(), t.spinnerModel.Tick)
}

func (t *SpinnerCmd) start() tea.Cmd {
	return func() tea.Msg {
		p, err := t.cmd.StdoutPipe()
		if err != nil {
			return finishedError{err}
		}
		go t.watchOutput(t.stdout, p)

		p, err = t.cmd.StderrPipe()
		if err != nil {
			return finishedError{err}
		}
		go t.watchOutput(t.stderr, p)

		return finishedError{t.cmd.Start()}
	}
}

func (t *SpinnerCmd) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			t.err = fmt.Errorf("quit by %s", msg.String())
			return t, tea.Quit
		default:
			return t, nil
		}
	case finishedError:
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

	// Make sure buf length not exceed screen width
	maxWidth := int(t.size.Width) - 2 - len(s) - 1 - len(t.title) - 1
	if len(t.buf) > maxWidth {
		t.buf = append(t.buf[:maxWidth-3], []byte("...")...)
	}

	return fmt.Sprintf(spinnerCmdTemplate, s, t.title, t.buf)
}

const spinnerCmdTemplate = `
  %s %s %s

     (esc/q/ctrl+c to quit)

`

func (t *SpinnerCmd) Run() (err error) {
	if t.size, err = checkConsole(); err != nil {
		return err
	}

	if _, err = t.p.Run(); err != nil {
		return err
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
	defer func() {
		if err := rc.Close(); err != nil {
			t.errCh <- err
		}
	}()
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
