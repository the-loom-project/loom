package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type popup interface {
	update(a *App, msg tea.Msg) (popup, tea.Cmd)
	title(a *App) string
	hints(a *App) string
	size(a *App) (w, h int)
	body(a *App, w, h int) []string
}

func box(title, hints string, body []string, w, h int) []string {
	inner := w - 4
	top := sEdge.Render("╭─ ") + sBold.Render(clip(title, w-8)) + " "
	top += sEdge.Render(strings.Repeat("─", max(0, w-width(top)-1)) + "╮")
	out := []string{top}
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		out = append(out, sEdge.Render("│")+" "+fit(line, inner)+" "+sEdge.Render("│"))
	}
	hint := ""
	if hints != "" {
		hint = " " + clip(hints, w-8) + " "
	}
	bottom := sEdge.Render("╰─") + hint
	bottom += sEdge.Render(strings.Repeat("─", max(0, w-width(bottom)-1)) + "╯")
	return append(out, bottom)
}

func composite(base []string, win []string, screenW int) []string {
	out := make([]string, len(base))
	for i, l := range base {
		out[i] = sDim.Render(ansi.Strip(l))
	}
	h, w := len(win), width(win[0])
	y0, x0 := max(2, (len(base)-h)/2), max(0, (screenW-w)/2)
	for i, wl := range win {
		y := y0 + i
		if y >= len(out) {
			break
		}
		line := ansi.Strip(base[y])
		line += strings.Repeat(" ", max(0, screenW-width(line)))
		out[y] = sDim.Render(ansi.Truncate(line, x0, "")) + wl + sDim.Render(ansi.TruncateLeft(line, x0+w, ""))
	}
	return out
}
