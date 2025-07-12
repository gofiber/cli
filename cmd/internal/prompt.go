package internal

import (
	"fmt"
	"os"

	input "github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

type errMsg error

// Prompt represents a small interactive input prompt used in the CLI.
type Prompt struct {
	err       error
	p         *tea.Program
	title     string
	answer    string
	textInput input.Model
}

// NewPrompt initializes a new Prompt with an optional placeholder value.
func NewPrompt(title string, placeholder ...string) *Prompt {
	p := &Prompt{
		title:     title,
		textInput: input.New(),
	}

	if len(placeholder) > 0 {
		p.textInput.Placeholder = placeholder[0]
	}

	p.p = tea.NewProgram(p, tea.WithOutput(termenv.NewOutput(os.Stdout)))

	return p
}

// YesOrNo runs the prompt and returns true if the answer resembles "yes".
func (p *Prompt) YesOrNo() (bool, error) {
	answer, err := p.Answer()
	if err != nil {
		return false, err
	}

	return parseBool(answer), nil
}

// parseBool returns true if the provided string represents a truthy value.
func parseBool(str string) bool {
	switch str {
	case "1", "t", "T", "true", "TRUE", "True", "y", "Y", "yes", "Yes":
		return true
	}
	return false
}

// Answer displays the prompt and returns the user's input.
func (p *Prompt) Answer() (result string, err error) {
	if _, err = checkConsole(); err != nil {
		return "", fmt.Errorf("check console: %w", err)
	}

	if _, err := p.p.Run(); err != nil {
		return "", fmt.Errorf("run prompt: %w", err)
	}
	return p.answer, nil
}

// Init initializes the bubbletea program for the prompt.
func (p *Prompt) Init() tea.Cmd {
	p.textInput.Focus()

	return input.Blink
}

// Update handles prompt events and updates its state.
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
		default:
			// ignore other keys
		}

	// We handle errors just like any other message
	case errMsg:
		p.err = msg
		return p, nil
	}

	p.textInput, cmd = p.textInput.Update(msg)
	return p, cmd
}

// View renders the prompt UI.
func (p *Prompt) View() string {
	return fmt.Sprintf(
		"%s\n\n%s\n\n%s\n\n",
		p.title,
		p.textInput.View(),
		"(esc to quit)",
	)
}
