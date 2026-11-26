package tui

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/hf"
	"loom/internal/hw"
	"loom/internal/keys"
	"loom/internal/llama"
	"loom/internal/store"
)

type tabID int

const (
	tabLibrary tabID = iota
	tabDiscover
	tabActivity
	tabDoctor
	tabProfiles
)

var tabNames = [...]string{"Library", "Discover", "Activity", "Doctor", "Profiles"}

type page interface {
	update(a *App, msg tea.Msg) tea.Cmd
	view(a *App, w, h int) []string
	hints(a *App) string
	typing() bool
}

type App struct {
	st       *store.Store
	hub      *hf.Client
	profiles *llama.Profiles
	keys     *keys.Map

	hw      hw.Hardware
	hwKnown bool
	models  []store.Model
	others  []store.Other
	scanned bool

	w, h   int
	tab    tabID
	lib    *library
	disc   *discover
	act    *activity
	doc    *doctor
	prof   *profilesView
	popups []popup

	downloads []*download
	servers   []*llama.Instance
	nextID    int

	tick   int
	status status
}

type status struct {
	text string
	bad  bool
	at   time.Time
}

func Run(st *store.Store, hub *hf.Client, profiles *llama.Profiles, km *keys.Map) error {
	a := &App{st: st, hub: hub, profiles: profiles, keys: km, nextID: 1}
	a.lib, a.disc, a.act, a.doc = newLibrary(), newDiscover(), newActivity(), &doctor{}
	a.prof = &profilesView{}
	if len(km.Problems) > 0 {
		a.complain("%s", km.Problems[0])
	}
	p := tea.NewProgram(a, tea.WithAltScreen())

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		<-hup
		p.Quit()
	}()
	_, err := p.Run()
	signal.Stop(hup)
	for _, s := range a.servers {
		s.Stop()
	}
	for _, d := range a.downloads {
		d.cancel()
	}
	return err
}

type (
	hwMsg   hw.Hardware
	scanMsg struct {
		models []store.Model
		others []store.Other
	}
	tickMsg time.Time
)

func (a *App) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return hwMsg(hw.Detect()) },
		a.rescan(),
		probeEnv(a.st),
		a.disc.init(a),
		tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }),
	)
}

func (a *App) rescan() tea.Cmd {
	return func() tea.Msg {
		m, o := a.st.Scan()
		return scanMsg{m, o}
	}
}

func (a *App) env(m store.Model) llama.Env { return llama.Env{Model: m, HW: a.hw} }

func (a *App) say(format string, args ...any) {
	a.status = status{text: fmt.Sprintf(format, args...), at: time.Now()}
}

func (a *App) complain(format string, args ...any) {
	a.status = status{text: fmt.Sprintf(format, args...), bad: true, at: time.Now()}
}

func (a *App) push(p popup) { a.popups = append(a.popups, p) }

func (a *App) is(msg tea.KeyMsg, actions ...string) bool { return a.keys.Any(msg.String(), actions...) }

func (a *App) hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		key := pairs[i]
		if l := a.keys.Label(key); l != "" {
			key = l
		}
		parts = append(parts, sBold.Render(key)+" "+sDim.Render(pairs[i+1]))
	}
	return strings.Join(parts, sDim.Render("  ·  "))
}

func (a *App) page() page {
	switch a.tab {
	case tabLibrary:
		return a.lib
	case tabDiscover:
		return a.disc
	case tabDoctor:
		return a.doc
	case tabProfiles:
		return a.prof
	}
	return a.act
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		return a, nil
	case hwMsg:
		a.hw, a.hwKnown = hw.Hardware(msg), true
		return a, nil
	case scanMsg:
		a.models, a.others, a.scanned = msg.models, msg.others, true
		return a, nil
	case tickMsg:
		a.tick++
		if !a.status.at.IsZero() && time.Since(a.status.at) > 6*time.Second {
			a.status = status{}
		}
		return a, tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, a.quit()
		}
	}

	if n := len(a.popups); n > 0 {
		return a, a.updatePopups(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		p := a.page()
		switch {
		case a.is(key, "next-view") && !(p.typing() && key.String() != "tab"):
			a.tab = (a.tab + 1) % tabID(len(tabNames))
			return a, nil
		case a.is(key, "prev-view") && !(p.typing() && key.String() != "shift+tab"):
			a.tab = (a.tab + tabID(len(tabNames)) - 1) % tabID(len(tabNames))
			return a, nil
		case p.typing():
		case a.is(key, "quit"):
			return a, a.quit()
		case a.is(key, "help"):
			a.push(&helpView{})
			return a, nil
		case a.is(key, "view-library"):
			a.tab = tabLibrary
			return a, nil
		case a.is(key, "view-discover"):
			a.tab = tabDiscover
			return a, nil
		case a.is(key, "view-activity"):
			a.tab = tabActivity
			return a, nil
		case a.is(key, "view-doctor"):
			a.tab = tabDoctor
			return a, nil
		case a.is(key, "view-profiles"):
			a.tab = tabProfiles
			return a, nil
		}
		return a, p.update(a, msg)
	}
	return a, tea.Batch(a.lib.update(a, msg), a.disc.update(a, msg), a.act.update(a, msg), a.doc.update(a, msg))
}

func (a *App) updatePopups(msg tea.Msg) tea.Cmd {
	n := len(a.popups)
	top, cmd := a.popups[n-1].update(a, msg)
	if top == nil {
		a.popups = append(a.popups[:n-1], a.popups[n:]...)
	} else {
		a.popups[n-1] = top
	}
	if _, isKey := msg.(tea.KeyMsg); isKey {
		return cmd
	}
	cmds := []tea.Cmd{cmd}
	for i := n - 2; i >= 0 && i < len(a.popups); i-- {
		if p, c := a.popups[i].update(a, msg); p != nil {
			a.popups[i] = p
			cmds = append(cmds, c)
		}
	}
	cmds = append(cmds, a.lib.update(a, msg), a.disc.update(a, msg), a.act.update(a, msg), a.doc.update(a, msg))
	return tea.Batch(cmds...)
}

func (a *App) quit() tea.Cmd {
	running, active := 0, 0
	for _, s := range a.servers {
		if s.Alive() {
			running++
		}
	}
	for _, d := range a.downloads {
		if d.active() {
			active++
		}
	}
	if running+active == 0 {
		return tea.Quit
	}
	var what []string
	if running > 0 {
		what = append(what, fmt.Sprintf("stop %d running server(s)", running))
	}
	if active > 0 {
		what = append(what, fmt.Sprintf("cancel %d download(s) (they resume next time)", active))
	}
	a.push(&confirm{heading: "Quit loom?", text: "This will " + strings.Join(what, " and ") + ".", yes: func(a *App) tea.Cmd { return tea.Quit }})
	return nil
}

func (a *App) View() string {
	if a.w == 0 {
		return ""
	}
	bodyH := a.h - 3
	var lines []string
	for _, l := range a.page().view(a, a.w, bodyH) {
		lines = append(lines, l)
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	screen := append([]string{a.header(), a.tabRule()}, lines[:bodyH]...)
	screen = append(screen, a.footer())

	if n := len(a.popups); n > 0 {
		p := a.popups[n-1]
		pw, ph := p.size(a)
		pw, ph = min(pw, a.w-2), min(ph, a.h-4)
		screen = composite(screen, box(p.title(a), p.hints(a), p.body(a, pw-4, ph-2), pw, ph), a.w)
	}
	return strings.Join(screen, "\n")
}

func (a *App) header() string {
	var tabs []string
	for i, name := range tabNames {
		label := name
		switch tabID(i) {
		case tabLibrary:
			if a.scanned {
				label += sDim.Render(fmt.Sprintf(" %d", len(a.models)))
			}
		case tabActivity:
			if n := a.liveCount(); n > 0 {
				label += sGood.Render(fmt.Sprintf(" %d", n))
			}
		}
		if tabID(i) == a.tab {
			tabs = append(tabs, sBold.Render(name)+strings.TrimPrefix(label, name))
		} else {
			tabs = append(tabs, sDim.Render(name)+strings.TrimPrefix(label, name))
		}
	}
	left := " " + sAccent.Bold(true).Render("loom") + "   " + strings.Join(tabs, "   ")
	return left
}

func (a *App) tabRule() string {
	x := 1 + 4 + 3
	start, end := x, x
	for i, name := range tabNames {
		label := name
		switch tabID(i) {
		case tabLibrary:
			if a.scanned {
				label += fmt.Sprintf(" %d", len(a.models))
			}
		case tabActivity:
			if n := a.liveCount(); n > 0 {
				label += fmt.Sprintf(" %d", n)
			}
		}
		if tabID(i) == a.tab {
			start, end = x, x+width(label)
		}
		x += width(label) + 3
	}
	end = min(end, a.w)
	return sEdge.Render(strings.Repeat("─", start)) + sAccent.Render(strings.Repeat("━", end-start)) + sEdge.Render(strings.Repeat("─", max(0, a.w-end)))
}

func (a *App) liveCount() int {
	n := 0
	for _, s := range a.servers {
		if s.Alive() {
			n++
		}
	}
	for _, d := range a.downloads {
		if d.active() {
			n++
		}
	}
	return n
}

func (a *App) footer() string {
	hints := " " + a.page().hints(a)
	if len(a.popups) > 0 {
		hints = ""
	}
	if a.status.text == "" {
		return hints
	}
	st := sGood
	if a.status.bad {
		st = sBad
	}
	return between(hints, st.Render(a.status.text)+" ", a.w)
}

func (a *App) isRunning(ref string) bool {
	for _, s := range a.servers {
		if s.Alive() && s.Model.Ref == ref {
			return true
		}
	}
	return false
}
