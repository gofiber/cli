package internal

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

var term = termenv.ColorProfile()

type Task func() error

type SpinnerTask struct {
	p            *tea.Program
	spinnerModel spinner.Model
	err          error
	title        string
	task         Task
}

type finishedMsg struct{ error }

func NewSpinnerTask(title string, task Task) *SpinnerTask {
	spinnerModel := spinner.NewModel()
	spinnerModel.Frames = spinner.Dot

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
		}, spinner.Tick(t.spinnerModel))
}

func (t *SpinnerTask) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		t.err = msg.error
		return t, tea.Quit

	default:
		var cmd tea.Cmd
		t.spinnerModel, cmd = spinner.Update(msg, t.spinnerModel)
		return t, cmd
	}

}

func (t *SpinnerTask) View() string {
	if t.err != nil {
		return ""
	}

	s := termenv.
		String(spinner.View(t.spinnerModel)).
		Foreground(term.Color("205")).
		String()

	return fmt.Sprintf("\n   %s %s\n\n(esc/q/ctrl+c to quit)\n\n", s, t.title)
}

func (t *SpinnerTask) Run() (err error) {
	if err = checkConsole(); err != nil {
		return
	}

	if err = t.p.Start(); err != nil {
		return
	}

	return t.err
}
