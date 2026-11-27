package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

type option struct {
	value string
	label string
	help  string
}

type picker struct {
	heading string
	options []option
	sc      scroller
	pick    func(a *App, value string) tea.Cmd
}

func newPicker(heading string, options []option, current string, pick func(a *App, value string) tea.Cmd) *picker {
	p := &picker{heading: heading, options: options, pick: pick}
	for i, o := range options {
		if o.value == current {
			p.sc.cur = i
		}
	}
	return p
}

func (p *picker) title(a *App) string { return p.heading }

func (p *picker) hints(a *App) string {
	return a.hints("confirm", "choose", "back", "cancel")
}

func (p *picker) labelWidth() int {
	w := 8
	for _, o := range p.options {
		w = max(w, width(o.label))
	}
	return min(w, 30) + 2
}

func (p *picker) size(a *App) (int, int) {
	help := 0
	for _, o := range p.options {
		help = max(help, width(o.help))
	}
	return min(a.w-8, max(52, p.labelWidth()+help+8)), len(p.options) + 5
}

func (p *picker) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch {
	case a.is(key, "up"):
		p.sc.move(-1, len(p.options))
	case a.is(key, "down"):
		p.sc.move(1, len(p.options))
	case a.is(key, "top"):
		p.sc.cur = 0
	case a.is(key, "bottom"):
		p.sc.cur = len(p.options) - 1
	case a.is(key, "back", "quit"):
		return nil, nil
	case a.is(key, "confirm", "right"):
		return nil, p.pick(a, p.options[p.sc.cur].value)
	}
	return p, nil
}

func (p *picker) body(a *App, w, h int) []string {
	lines := []string{""}
	lw := p.labelWidth()
	from, to := p.sc.window(len(p.options), max(1, h-1))
	for i := from; i < to; i++ {
		o := p.options[i]
		if i == p.sc.cur {
			lines = append(lines, sPlain.Background(cSel).Bold(true).Render(fit(" "+o.label, lw+1))+sPlain.Background(cSel).Foreground(cDim).Render(fit(o.help, w-lw-1)))
			continue
		}
		lines = append(lines, " "+fit(o.label, lw)+sDim.Render(o.help))
	}
	return lines
}
