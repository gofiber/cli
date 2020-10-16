package internal

import (
	"fmt"
	"testing"

	input "github.com/charmbracelet/bubbles/textinput"

	"github.com/stretchr/testify/assert"
)

func Test_Prompt_New(t *testing.T) {
	t.Parallel()

	at := assert.New(t)

	p := NewPrompt("title", "placeholder")

	at.NotNil(p)
	at.Equal("title", p.title)
	at.Equal("placeholder", p.placeholder)
	at.NotNil(p.p)
}

func Test_Prompt_Answer(t *testing.T) {
	p := NewPrompt("")
	_, err := p.Answer()
	assert.NotNil(t, err)
}

func Test_Prompt_YesOrNo(t *testing.T) {
	p := NewPrompt("")
	_, err := p.YesOrNo()
	assert.NotNil(t, err)
}

func Test_Prompt_ParseBool(t *testing.T) {
	assert.True(t, parseBool("y"))
	assert.False(t, parseBool(""))
}

func Test_Prompt_Initialize(t *testing.T) {
	at := assert.New(t)

	p := NewPrompt("")
	m, cmd := p.initialize()
	_, ok := m.(Prompt)

	at.True(ok)

	switch cmd().(type) {
	case input.BlinkMsg:
	default:
		at.Fail("msg should be input.BlankMsg")
	}
}

func Test_Prompt_Update(t *testing.T) {
	t.Skip("Finish it later")
}

func Test_Prompt_View(t *testing.T) {
	at := assert.New(t)
	p := NewPrompt("")

	at.Equal("Oh no: could not perform assertion on model.", p.view(p))

	at.Contains(p.view(*p), "esc")

	p.err = fmt.Errorf("fake error")
	at.Equal("Uh oops: fake error", p.view(*p))
}
