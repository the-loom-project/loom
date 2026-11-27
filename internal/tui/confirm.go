package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

type confirm struct {
	heading string
	text    string
	yes     func(a *App) tea.Cmd
}

func (c *confirm) title(a *App) string { return c.heading }

func (c *confirm) hints(a *App) string { return a.hints("y", "yes", "n", "no") }

func (c *confirm) size(a *App) (int, int) {
	w := min(a.w-8, 72)
	return w, len(wrap(c.text, w-4)) + 4
}

func (c *confirm) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.String() == "y" || key.String() == "Y" || a.is(key, "confirm"):
			return nil, c.yes(a)
		case key.String() == "n" || key.String() == "N" || a.is(key, "back", "quit"):
			return nil, nil
		}
	}
	return c, nil
}

func (c *confirm) body(a *App, w, h int) []string {
	lines := []string{""}
	for _, l := range wrap(c.text, w) {
		lines = append(lines, l)
	}
	return lines
}
