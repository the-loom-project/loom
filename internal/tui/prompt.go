package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type namePrompt struct {
	heading string
	input   textinput.Model
	done    func(a *App, name string)
}

func newNamePrompt(heading, initial string, done func(a *App, name string)) (*namePrompt, tea.Cmd) {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 40
	ti.Placeholder = "e.g. fast, long-context"
	ti.SetValue(initial)
	ti.CursorEnd()
	return &namePrompt{heading: heading, input: ti, done: done}, ti.Focus()
}

func (p *namePrompt) title(a *App) string { return p.heading }

func (p *namePrompt) hints(a *App) string { return a.hints("confirm", "accept", "back", "cancel") }

func (p *namePrompt) size(a *App) (int, int) { return 52, 6 }

func (p *namePrompt) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch {
		case a.is(key, "back"):
			return nil, nil
		case a.is(key, "confirm"):
			if name := strings.TrimSpace(p.input.Value()); name != "" {
				p.done(a, name)
				return nil, nil
			}
			return p, nil
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return p, cmd
}

func (p *namePrompt) body(a *App, w, h int) []string {
	return []string{"", " " + sDim.Render("Name") + "  " + p.input.View()}
}
