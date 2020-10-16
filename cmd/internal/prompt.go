package internal

import (
	"errors"
	"fmt"

	input "github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/containerd/console"
)

type Prompt struct {
	p           *tea.Program
	textInput   input.Model
	err         error
	title       string
	placeholder string
	answer      string
}

type tickMsg struct{}
type errMsg error

func NewPrompt(title string, placeholder ...string) *Prompt {
	p := &Prompt{
		title: title,
	}

	if len(placeholder) > 0 {
		p.placeholder = placeholder[0]
	}

	p.p = tea.NewProgram(p.initialize, p.update, p.view)

	return p
}

func (p *Prompt) YesOrNo() (bool, error) {
	if _, err := p.Answer(); err != nil {
		return false, err
	}

	return parseBool(p.answer), nil
}

func parseBool(str string) bool {
	switch str {
	case "1", "t", "T", "true", "TRUE", "True", "y", "Y", "yes", "Yes":
		return true
	}
	return false
}

func (p *Prompt) Answer() (result string, err error) {
	if err = checkConsole(); err != nil {
		return
	}

	if err := p.p.Start(); err != nil {
		return "", err
	}
	return p.answer, nil
}

func (p *Prompt) initialize() (tea.Model, tea.Cmd) {
	inputModel := input.NewModel()
	inputModel.Placeholder = p.placeholder
	inputModel.Focus()

	return Prompt{
		textInput: inputModel,
		err:       nil,
	}, input.Blink(inputModel)
}

func (p *Prompt) update(msg tea.Msg, model tea.Model) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m, ok := model.(Prompt)
	if !ok {
		// When we encounter errors in Update we simply add the error to the
		// model so we can handle it in the view. We could also return a command
		// that does something else with the error, like logs it via IO.
		return Prompt{
			err: errors.New("could not perform assertion on model in update"),
		}, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			fallthrough
		case tea.KeyEsc:
			fallthrough
		case tea.KeyEnter:
			p.answer = m.textInput.Value()
			return m, tea.Quit
		}

	// We handle errors just like any other message
	case errMsg:
		m.err = msg
		return m, nil
	}

	m.textInput, cmd = input.Update(msg, m.textInput)
	return m, cmd
}

func (p *Prompt) view(model tea.Model) string {
	m, ok := model.(Prompt)
	if !ok {
		return "Oh no: could not perform assertion on model."
	} else if m.err != nil {
		return fmt.Sprintf("Uh oops: %s", m.err)
	}

	return fmt.Sprintf(
		"%s\n\n%s\n\n%s\n\n",
		p.title,
		input.View(m.textInput),
		"(esc to quit)",
	)
}

func checkConsole() (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()

	console.Current()

	return
}
