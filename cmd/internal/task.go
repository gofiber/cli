package internal

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

type Task func() error

type SpinnerTask struct {
	err          error
	p            *tea.Program
	task         Task
	title        string
	spinnerModel spinner.Model
}

func NewSpinnerTask(title string, task Task) *SpinnerTask {
	spinnerModel := spinner.New()
	spinnerModel.Spinner = spinner.Dot

	at := &SpinnerTask{
		title:        title,
		spinnerModel: spinnerModel,
		task:         task,
	}

	at.p = tea.NewProgram(at)

	return at
}

func (t *SpinnerTask) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			return finishedMsg{t.task()}
		}, t.spinnerModel.Tick)
}

func (t *SpinnerTask) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			t.err = fmt.Errorf("quit by %s", msg.String())
			return t, tea.Quit
		default:
			return t, nil
		}

	case finishedMsg:
		t.err = msg.error
		return t, tea.Quit

	default:
		var cmd tea.Cmd
		t.spinnerModel, cmd = t.spinnerModel.Update(msg)
		return t, cmd
	}
}

func (t *SpinnerTask) View() string {
	if t.err != nil {
		return ""
	}

	s := termenv.
		String(t.spinnerModel.View()).
		Foreground(term.Color("205")).
		String()

	return fmt.Sprintf("\n   %s %s\n\n(esc/q/ctrl+c to quit)\n\n", s, t.title)
}

func (t *SpinnerTask) Run() (err error) {
	if _, err = checkConsole(); err != nil {
		return err
	}

	if _, err = t.p.Run(); err != nil {
		return fmt.Errorf("run spinner: %w", err)
	}

	return t.err
}
