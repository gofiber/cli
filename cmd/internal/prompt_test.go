package internal

import (
	"errors"
	"testing"

	input "github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func Test_Prompt_New(t *testing.T) {
	t.Parallel()

	at := assert.New(t)

	p := NewPrompt("title", "placeholder")

	at.NotNil(p)
	at.Equal("title", p.title)
	at.Equal("placeholder", p.textInput.Placeholder)
	at.NotNil(p.p)
}

func Test_Prompt_Answer(t *testing.T) {
	t.Parallel()

	p := NewPrompt("")
	_, err := p.Answer()
	assert.NotNil(t, err)
}

func Test_Prompt_YesOrNo(t *testing.T) {
	t.Parallel()

	p := NewPrompt("")
	_, err := p.YesOrNo()
	assert.NotNil(t, err)
}

func Test_Prompt_ParseBool(t *testing.T) {
	t.Parallel()

	assert.True(t, parseBool("y"))
	assert.False(t, parseBool(""))
}

func Test_Prompt_Initialize(t *testing.T) {
	t.Parallel()

	at := assert.New(t)

	p := NewPrompt("")
	cmd := p.Init()

	switch cmd().(type) {
	case input.BlinkMsg:
	default:
		at.Fail("msg should be input.BlankMsg")
	}
}

func Test_Prompt_Update(t *testing.T) {
	t.Parallel()

	at := assert.New(t)

	p := NewPrompt("")

	var cmd tea.Cmd
	_, cmd = p.Update(nil)
	at.Nil(cmd)

	_, cmd = p.Update(errMsg(errors.New("fake error")))
	at.NotNil(p.err)
	at.Nil(cmd)

	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyRune, Rune: 'a'})
	at.Nil(cmd)

	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	at.NotNil(cmd)
}

func Test_Prompt_View(t *testing.T) {
	t.Parallel()

	at := assert.New(t)
	p := NewPrompt("")

	at.Contains(p.View(), "esc")
}
