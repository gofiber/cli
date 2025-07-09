package internal

import (
	"fmt"
	"os"

	input "github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

type errMsg error

type Prompt struct {
	p         *tea.Program
	textInput input.Model
	err       error
	title     string
	answer    string
}

func NewPrompt(title string, placeholder ...string) *Prompt {
	p := &Prompt{
		title:     title,
		textInput: input.NewModel(),
	}

	if len(placeholder) > 0 {
		p.textInput.Placeholder = placeholder[0]
	}

	p.p = tea.NewProgram(p, tea.WithOutput(termenv.NewOutput(os.Stdout)))

	return p
}

func (p *Prompt) YesOrNo() (bool, error) {
	answer, err := p.Answer()
	if err != nil {
		return false, err
	}

	return parseBool(answer), nil
}

func parseBool(str string) bool {
	switch str {
	case "1", "t", "T", "true", "TRUE", "True", "y", "Y", "yes", "Yes":
		return true
	}
	return false
}

func (p *Prompt) Answer() (result string, err error) {
	if _, err = checkConsole(); err != nil {
		return
	}

	if err := p.p.Start(); err != nil {
		return "", err
	}
	return p.answer, nil
}

func (p *Prompt) Init() tea.Cmd {
	p.textInput.Focus()

	return input.Blink
}

func (p *Prompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			fallthrough
		case tea.KeyEsc:
			fallthrough
		case tea.KeyEnter:
			p.answer = p.textInput.Value()
			return p, tea.Quit
		}

	// We handle errors just like any other message
	case errMsg:
		p.err = msg
		return p, nil
	}

	p.textInput, cmd = p.textInput.Update(msg)
	return p, cmd
}

func (p *Prompt) View() string {
	return fmt.Sprintf(
		"%s\n\n%s\n\n%s\n\n",
		p.title,
		p.textInput.View(),
		"(esc to quit)",
	)
}
