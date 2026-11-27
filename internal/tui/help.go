package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/keys"
)

type helpView struct{ sc scroller }

func (v *helpView) title(a *App) string { return "Keys" }

func (v *helpView) hints(a *App) string {
	return a.hints("up", "scroll", "back", "close") + sDim.Render("  ·  customize: loom keys")
}

func (v *helpView) size(a *App) (int, int) { return min(a.w-8, 150), min(a.h-6, 40) }

func (v *helpView) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch {
		case a.is(key, "back", "quit", "help"):
			return nil, nil
		case a.is(key, "up"):
			v.sc.cur = max(0, v.sc.cur-1)
		case a.is(key, "down"):
			v.sc.cur++
		}
	}
	return v, nil
}

func (v *helpView) body(a *App, w, h int) []string {

	var blocks [][]string
	var cur []string
	group := ""
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, cur)
		}
	}
	for _, act := range keys.Actions {
		if act.Group != group {
			flush()
			group = act.Group
			cur = []string{heading(group, w/2-3)}
		}
		cur = append(cur, sBold.Render(fit(a.keys.Labels(act.ID), 14))+sDim.Render(act.Help))
	}
	flush()
	total := 0
	for _, b := range blocks {
		total += len(b) + 1
	}
	var cols [2][]string
	c := 0
	for _, b := range blocks {
		if c == 0 && len(cols[0])+len(b) > (total+1)/2 && len(cols[0]) > 0 {
			c = 1
		}
		cols[c] = append(append(cols[c], b...), "")
	}
	lines := []string{""}
	for i := 0; i < max(len(cols[0]), len(cols[1])); i++ {
		l, r := "", ""
		if i < len(cols[0]) {
			l = cols[0][i]
		}
		if i < len(cols[1]) {
			r = cols[1][i]
		}
		lines = append(lines, " "+fit(l, w/2-1)+" "+r)
	}
	lines = append(lines, "", " "+sDim.Render("Change any of these in "+home(keys.Path())+", or run `loom keys` to see the format."))
	v.sc.cur = min(v.sc.cur, max(0, len(lines)-h))
	return lines[min(v.sc.cur, max(0, len(lines)-1)):]
}
