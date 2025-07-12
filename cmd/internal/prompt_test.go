package internal

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.Error(t, err)
}

func Test_Prompt_YesOrNo(t *testing.T) {
	t.Parallel()

	p := NewPrompt("")
	_, err := p.YesOrNo()
	require.Error(t, err)
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

	at.NotNil(cmd)
}

func Test_Prompt_Update(t *testing.T) {
	t.Parallel()

	at := assert.New(t)

	p := NewPrompt("")

	var cmd tea.Cmd
	_, cmd = p.Update(nil)
	at.Nil(cmd)

	_, cmd = p.Update(errMsg(errors.New("fake error")))
	require.Error(t, p.err)
	at.Nil(cmd)

	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
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
