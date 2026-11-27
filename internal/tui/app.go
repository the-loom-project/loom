package tui

import (
	"cmp"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"loom/internal/config"
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
	tabConfig
)

var (
	tabNames = [...]string{"Library", "Discover", "Activity", "Doctor", "Profiles", "Config"}
	tabKeys  = [...]string{"view-library", "view-discover", "view-activity", "view-doctor", "view-profiles", "view-config"}
)

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
	cfg      *config.Config
	warnings []string

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
	conf   *configView
	popups []popup

	downloads []*download
	latest    map[string]*hf.Repo
	checked   map[string]bool
	gate      chan struct{}
	servers   []*llama.Instance
	settled   map[*llama.Instance]bool
	nextID    int

	tick   int
	status status
}

type status struct {
	text string
	bad  bool
	at   time.Time
}

func Run(st *store.Store, hub *hf.Client, profiles *llama.Profiles, km *keys.Map, cfg *config.Config, warnings []string) error {
	a := &App{st: st, hub: hub, profiles: profiles, keys: km, cfg: cfg, warnings: warnings, nextID: 1, latest: map[string]*hf.Repo{}, checked: map[string]bool{}, settled: map[*llama.Instance]bool{}, gate: make(chan struct{}, 4)}
	a.lib, a.disc, a.act, a.doc = newLibrary(), newDiscover(), newActivity(), &doctor{}
	a.prof, a.conf = &profilesView{}, newConfigView()
	if problems := slices.Concat(warnings, km.Problems); len(problems) > 0 {
		a.complain("%s", problems[0])
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

func (a *App) env(m store.Model) llama.Env {
	return llama.Env{Model: m, HW: a.hw, Limits: a.cfg.Limits()}
}

func (a *App) say(format string, args ...any) {
	a.status = status{text: fmt.Sprintf(format, args...), at: time.Now()}
}

func (a *App) complain(format string, args ...any) {
	a.status = status{text: fmt.Sprintf(format, args...), bad: true, at: time.Now()}
}

func (a *App) failed(err error, what string) bool {
	if err != nil {
		a.complain("%s: %v", what, err)
	}
	return err != nil
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
	case tabConfig:
		return a.conf
	}
	return a.act
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.update(msg)
	if a.tab == tabDiscover {
		cmd = tea.Batch(cmd, a.disc.fetchSizes(a))
	}
	return a, cmd
}

func (a *App) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.lib.query.Width, a.disc.input.Width = searchWidth(a.w), searchWidth(a.w)
		return nil
	case hwMsg:
		a.hw, a.hwKnown = hw.Hardware(msg), true
		return nil
	case dlDoneMsg:
		return a.downloadDone(msg)
	case discardMsg:
		return a.discarded(msg)
	case scanMsg:
		a.models, a.others, a.scanned = msg.models, msg.others, true
		return a.checkLatest()
	case latestMsg:
		a.latestArrived(msg)
		return nil
	case tickMsg:
		a.tick++
		a.noteLoads()
		if !a.status.at.IsZero() && time.Since(a.status.at) > 6*time.Second {
			a.status = status{}
		}
		return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a.quit()
		}
	}

	if len(a.popups) > 0 {
		return a.updatePopups(msg)
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return tea.Batch(a.lib.update(a, msg), a.disc.update(a, msg), a.act.update(a, msg), a.doc.update(a, msg))
	}
	p := a.page()
	switch {
	case a.is(key, "next-view") && !(p.typing() && key.String() != "tab"):
		a.tab = (a.tab + 1) % tabID(len(tabNames))
		return nil
	case a.is(key, "prev-view") && !(p.typing() && key.String() != "shift+tab"):
		a.tab = (a.tab + tabID(len(tabNames)) - 1) % tabID(len(tabNames))
		return nil
	case p.typing():
	case a.is(key, "quit"):
		return a.quit()
	case a.is(key, "help"):
		a.push(&helpView{})
		return nil
	default:
		for i, id := range tabKeys {
			if a.is(key, id) {
				a.tab = tabID(i)
				return nil
			}
		}
	}
	return p.update(a, msg)
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

const minWidth, minHeight = 40, 10

func (a *App) View() string {
	switch {
	case a.w == 0:
		return ""
	case a.w < minWidth || a.h < minHeight:
		return a.tooSmall()
	}
	bodyH := a.h - 3
	lines := a.page().view(a, a.w, bodyH)
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	header, rule := a.tabBar()
	screen := append([]string{header, rule}, lines[:bodyH]...)
	screen = append(screen, a.footer())

	if n := len(a.popups); n > 0 {
		p := a.popups[n-1]
		pw, ph := a.popupSize(p)
		screen = composite(screen, box(p.title(a), p.hints(a), p.body(a, pw-4, ph-2), pw, ph), a.w)
	}
	return strings.Join(screen, "\n")
}

func (a *App) popupSize(p popup) (int, int) {
	w, h := p.size(a)
	return min(w, a.w-2), min(h, a.h-4)
}

func (a *App) popupBody(p popup) (int, int) {
	w, h := a.popupSize(p)
	return w - 4, h - 2
}

func (a *App) tooSmall() string {
	msg := []string{clip(sBold.Render("Not enough screen space"), a.w)}
	if a.h >= 3 {
		msg = append(msg, clip(sDim.Render(fmt.Sprintf("make it at least %d×%d", minWidth, minHeight)), a.w))
	}
	return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, msg...))
}

type tabLabel struct {
	name, key, count string
	countStyle       lipgloss.Style
}

func (a *App) tabLabels() []tabLabel {
	out := make([]tabLabel, len(tabNames))
	for i, name := range tabNames {
		out[i] = tabLabel{name: name, key: cmp.Or(a.keys.Label(tabKeys[i]), name[:1])}
	}
	if a.scanned {
		out[tabLibrary].count, out[tabLibrary].countStyle = fmt.Sprint(len(a.models)), sDim
	}
	if n := a.liveCount(); n > 0 {
		out[tabActivity].count, out[tabActivity].countStyle = fmt.Sprint(n), sGood
	}
	return out
}

func (a *App) tabBar() (header, rule string) {
	labels := a.tabLabels()
	text := func(i int, short bool) (string, string) {
		switch {
		case short && tabID(i) != a.tab:
			return labels[i].key, ""
		case labels[i].count == "":
			return labels[i].name, ""
		}
		return labels[i].name, " " + labels[i].count
	}
	layouts := []struct {
		brand, short bool
		gap          int
	}{{true, false, 3}, {false, false, 2}, {false, true, 2}}
	for n, l := range layouts {
		x, start, end := 1, 0, 0
		head := " "
		if l.brand {
			head += sAccent.Bold(true).Render("loom") + "   "
			x += 7
		}
		for i := range labels {
			if i > 0 {
				head += strings.Repeat(" ", l.gap)
				x += l.gap
			}
			name, count := text(i, l.short)
			st := sDim
			if tabID(i) == a.tab {
				st, start, end = sBold, x, x+width(name+count)
			}
			head += st.Render(name) + labels[i].countStyle.Render(count)
			x += width(name + count)
		}
		if x <= a.w || n == len(layouts)-1 {
			end = min(end, a.w)
			rule = sEdge.Render(strings.Repeat("─", start)) + sAccent.Render(strings.Repeat("━", max(0, end-start))) + sEdge.Render(strings.Repeat("─", max(0, a.w-end)))
			return clip(head, a.w), rule
		}
	}
	return "", ""
}

func (a *App) liveCount() int {
	n := 0
	for _, s := range a.servers {
		if s.Alive() {
			n++
		}
	}
	return n
}

func (a *App) footer() string {
	hints := " " + a.page().hints(a)
	if len(a.popups) > 0 || !a.cfg.KeyHints {
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

func (a *App) noteLoads() {
	for _, s := range a.servers {
		st, _ := s.State()
		if a.settled[s] || st == llama.Loading {
			continue
		}
		a.settled[s] = true
		reason := s.LoadError()
		if st == llama.Ready || reason != "" {
			a.failed(a.profiles.SetFailure(s.Model.Ref, a.hw.Version, reason), "Couldn't remember how "+s.Model.Name()+" loaded")
		}
	}
}

func (a *App) isRunning(ref string) bool {
	for _, s := range a.servers {
		if s.Alive() && s.Model.Ref == ref {
			return true
		}
	}
	return false
}
