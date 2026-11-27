package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/llama"
	"loom/internal/units"
)

type activity struct {
	sc   scroller
	back map[*llama.Instance]int
}

func newActivity() *activity { return &activity{back: map[*llama.Instance]int{}} }

func (v *activity) typing() bool { return false }

func (v *activity) items(a *App) []*llama.Instance {
	out := make([]*llama.Instance, 0, len(a.servers))
	for i := len(a.servers) - 1; i >= 0; i-- {
		out = append(out, a.servers[i])
	}
	return out
}

func (v *activity) hints(a *App) string {
	return a.hints("confirm", "details", "open", "open in browser", "log-up", "scroll log", "delete", "stop", "copy", "copy cmd", "help", "all keys")
}

func (v *activity) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case chatDoneMsg:
		if msg.err != nil {
			a.complain("llama-cli exited: %v", msg.err)
		}
	case tea.KeyMsg:
		its := v.items(a)
		n := len(its)
		var cur *llama.Instance
		if v.sc.cur < n {
			cur = its[v.sc.cur]
		}
		switch {
		case a.is(msg, "up"):
			v.sc.move(-1, n)
		case a.is(msg, "down"):
			v.sc.move(1, n)
		case a.is(msg, "top"):
			v.sc.cur = 0
		case a.is(msg, "bottom"):
			v.sc.cur = max(0, n-1)
		case a.is(msg, "clear"):
			a.clearFinished()
		case a.is(msg, "confirm"):
			if cur != nil {
				a.push(&serverView{inst: cur})
			}
		case a.is(msg, "open"):
			if cur != nil && cur.Config.Mode == llama.Serve {
				openBrowser(cur.URL())
			}
		case a.is(msg, "log-up"):
			if cur != nil {
				v.back[cur] += 10
			}
		case a.is(msg, "log-down"):
			if cur != nil {
				v.back[cur] = max(0, v.back[cur]-10)
			}
		case a.is(msg, "delete"):
			v.remove(a, cur)
		case a.is(msg, "copy"), a.is(msg, "copy-raw"):
			if cur != nil {
				a.copyCommand(llama.Env{Model: cur.Model, HW: a.hw}, cur.Config, "", a.is(msg, "copy-raw"))
			}
		}
	}
	return nil
}

func (v *activity) remove(a *App, s *llama.Instance) {
	switch {
	case s == nil:
	case s.Alive():
		a.push(&confirm{heading: "Stop " + s.Model.Name() + "?", text: fmt.Sprintf("The server on port %d shuts down.", s.Port), yes: func(a *App) tea.Cmd { s.Stop(); return nil }})
	default:
		a.servers = without(a.servers, s)
	}
}

func without[T comparable](list []T, v T) []T {
	out := list[:0:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func (a *App) clearFinished() {
	var srv []*llama.Instance
	for _, s := range a.servers {
		if s.Alive() {
			srv = append(srv, s)
		}
	}
	a.servers = srv
}

func (v *activity) view(a *App, w, h int) []string {
	its := v.items(a)
	if len(its) == 0 {
		return []string{"", "", "  " + sBold.Render("Nothing running."), "",
			"  Run a model from " + sBold.Render("Library") + ", or get one from " + sBold.Render("Discover") + "."}
	}
	tb := newTable(w, column{title: "STATE", w: 12}, column{title: "NAME"}, column{title: "DETAILS", w: 64, min: 100})

	listH := min(len(its)+2, max(4, h*2/3-1))
	lines := []string{"", tb.header()}
	from, to := v.sc.window(len(its), max(1, listH-2))
	for i := from; i < to; i++ {
		lines = append(lines, serverRow(a, tb, its[i], i == v.sc.cur))
	}
	for len(lines) < listH {
		lines = append(lines, "")
	}

	return append(lines, v.panel(a, its[v.sc.cur], w, h-len(lines))...)
}

func (v *activity) panel(a *App, s *llama.Instance, w, h int) []string {
	if h < 3 {
		return nil
	}
	title := sBold.Render(s.Model.Name()) + sDim.Render("  log")
	if s.Config.Mode == llama.Serve {
		title += sDim.Render("  ·  ") + sFlag.Render(s.URL())
	}
	if n := v.back[s]; n > 0 {
		title += sDim.Render(fmt.Sprintf("  ·  scrolled %d lines up", n))
	}
	lines := []string{sEdge.Render(strings.Repeat("─", w)), " " + title}
	_, _, log := s.Snapshot(1000)
	room := max(1, h-len(lines))
	v.back[s] = min(v.back[s], max(0, len(log)-room))
	end := len(log) - v.back[s]
	for _, l := range log[max(0, end-room):end] {
		text, st := parseLog(l)
		lines = append(lines, " "+st.Render(clip(text, w-2)))
	}
	return lines
}

func serverRow(a *App, tb table, s *llama.Instance, sel bool) string {
	st, _, _ := s.Snapshot(0)
	state := map[llama.State][]seg{
		llama.Loading: styled("◌ loading", sWarn),
		llama.Ready:   styled("● ready", sGood),
		llama.Stopped: styled("○ stopped", sDim),
		llama.Failed:  styled("✗ failed", sBad),
	}[st]
	name := []seg{t(s.Model.Name(), sPlain)}
	if s.Model.Quant != "" {
		name = append(name, t("  "+s.Model.Quant, sDim))
	}
	where := ""
	if s.Config.Mode == llama.Serve {
		where = fmt.Sprintf(":%d  ", s.Port)
	}
	if st == llama.Loading || st == llama.Ready {
		where += "up " + units.Duration(time.Since(s.Started)) + "  "
	}
	return tb.row(sel, state, name, dim(where+summary(llama.Env{Model: s.Model, HW: a.hw}, s.Config)))
}

func openBrowser(url string) { exec.Command("xdg-open", url).Start() }
