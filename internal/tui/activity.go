package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/hf"
	"loom/internal/llama"
	"loom/internal/units"
)

type dlState int

const (
	dlRunning dlState = iota
	dlDone
	dlFailed
	dlCanceled
)

type download struct {
	repo, quant string
	started     time.Time
	cancel      context.CancelFunc

	mu   sync.Mutex
	prog hf.Progress
	st   dlState
	err  error
}

type dlDoneMsg struct {
	d   *download
	err error
}

func (d *download) set(p hf.Progress) { d.mu.Lock(); d.prog = p; d.mu.Unlock() }

func (d *download) snapshot() (hf.Progress, dlState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.prog, d.st, d.err
}

func (d *download) active() bool { _, st, _ := d.snapshot(); return st == dlRunning }

func pct(p hf.Progress) int {
	if p.Total == 0 {
		return 0
	}
	return int(100 * p.Done / p.Total)
}

func (a *App) download(repo, quant string) *download {
	for i := len(a.downloads) - 1; i >= 0; i-- {
		if d := a.downloads[i]; d.repo == repo && d.quant == quant {
			return d
		}
	}
	return nil
}

func (a *App) startDownload(repo *hf.Repo, q hf.Quant) tea.Cmd {
	if d := a.download(repo.ID, q.Name); d != nil && d.active() {
		a.say("%s:%s is already downloading", repo.ID, q.Name)
		return nil
	}
	files := append([]hf.File{}, q.Files...)
	if mm := repo.MMProj(); mm != nil {
		files = append(files, *mm)
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &download{repo: repo.ID, quant: q.Name, started: time.Now(), cancel: cancel, prog: hf.Progress{Total: total}}
	a.downloads = append(a.downloads, d)
	a.say("Downloading %s:%s, progress is in Activity", repo.ID, q.Name)
	hub, id, sha := a.st.Hub(), repo.ID, repo.SHA
	return func() tea.Msg {
		return dlDoneMsg{d, a.hub.Download(ctx, hub, id, sha, files, d.set)}
	}
}

type activity struct {
	sc   scroller
	back map[*llama.Instance]int
}

func newActivity() *activity { return &activity{back: map[*llama.Instance]int{}} }

func (v *activity) typing() bool { return false }

type item struct {
	srv *llama.Instance
	dl  *download
}

func (v *activity) items(a *App) []item {
	var out []item
	for i := len(a.servers) - 1; i >= 0; i-- {
		out = append(out, item{srv: a.servers[i]})
	}
	for i := len(a.downloads) - 1; i >= 0; i-- {
		out = append(out, item{dl: a.downloads[i]})
	}
	return out
}

func (v *activity) hints(a *App) string {
	return a.hints("confirm", "details", "open", "open in browser", "log-up", "scroll log", "stop", "stop", "copy", "copy cmd", "help", "all keys")
}

func (v *activity) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case dlDoneMsg:
		d := msg.d
		d.mu.Lock()
		switch {
		case msg.err == nil:
			d.st = dlDone
			d.prog.Done = d.prog.Total
		case errors.Is(msg.err, context.Canceled):
			d.st = dlCanceled
		default:
			d.st, d.err = dlFailed, msg.err
		}
		st := d.st
		d.mu.Unlock()
		switch st {
		case dlDone:
			a.say("✓ Downloaded %s:%s, it's in your Library", d.repo, d.quant)
		case dlFailed:
			a.complain("Download of %s:%s failed: %v", d.repo, d.quant, msg.err)
		}
		return a.rescan()
	case chatDoneMsg:
		if msg.err != nil {
			a.complain("llama-cli exited: %v", msg.err)
		}
	case tea.KeyMsg:
		its := v.items(a)
		n := len(its)
		var cur item
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
			if cur.srv != nil {
				a.push(&serverView{inst: cur.srv})
			}
		case a.is(msg, "open"):
			if cur.srv != nil && cur.srv.Config.Mode == llama.Serve {
				openBrowser(cur.srv.URL())
			}
		case a.is(msg, "log-up"):
			if cur.srv != nil {
				v.back[cur.srv] += 10
			}
		case a.is(msg, "log-down"):
			if cur.srv != nil {
				v.back[cur.srv] = max(0, v.back[cur.srv]-10)
			}
		case a.is(msg, "stop"):
			v.remove(a, cur)
		case a.is(msg, "copy"), a.is(msg, "copy-raw"):
			if cur.srv != nil {
				a.copyCommand(llama.Env{Model: cur.srv.Model, HW: a.hw}, cur.srv.Config, "", a.is(msg, "copy-raw"))
			}
		}
	}
	return nil
}

func (v *activity) remove(a *App, it item) {
	switch {
	case it.srv != nil && it.srv.Alive():
		a.push(&confirm{heading: "Stop " + it.srv.Model.Name() + "?", text: fmt.Sprintf("The server on port %d shuts down.", it.srv.Port), yes: func(a *App) tea.Cmd { it.srv.Stop(); return nil }})
	case it.srv != nil:
		a.servers = without(a.servers, it.srv)
	case it.dl != nil && it.dl.active():
		it.dl.cancel()
	case it.dl != nil:
		a.downloads = without(a.downloads, it.dl)
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
	var dls []*download
	for _, d := range a.downloads {
		if d.active() {
			dls = append(dls, d)
		}
	}
	a.servers, a.downloads = srv, dls
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
		if its[i].srv != nil {
			lines = append(lines, serverRow(a, tb, its[i].srv, i == v.sc.cur))
		} else {
			lines = append(lines, downloadRow(tb, its[i].dl, i == v.sc.cur))
		}
	}
	for len(lines) < listH {
		lines = append(lines, "")
	}

	var cur item
	if v.sc.cur < len(its) {
		cur = its[v.sc.cur]
	}
	return append(lines, v.panel(a, cur, w, h-len(lines))...)
}

func (v *activity) panel(a *App, it item, w, h int) []string {
	if h < 3 {
		return nil
	}
	if it.srv == nil {
		return v.downloadPanel(it.dl, w, h)
	}
	s := it.srv
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

func (v *activity) downloadPanel(d *download, w, h int) []string {
	p, st, err := d.snapshot()
	lines := []string{sEdge.Render(strings.Repeat("─", w)), " " + sBold.Render(d.repo+":"+d.quant) + sDim.Render("  download")}
	lines = append(lines, " "+sDim.Render("Started "+units.Duration(time.Since(d.started))+" ago"))
	switch st {
	case dlRunning:
		lines = append(lines, " "+fmt.Sprintf("%s of %s at %s/s", units.Bytes(p.Done), units.Bytes(p.Total), units.Bytes(int64(p.Speed))))
		if p.File != "" {
			lines = append(lines, " "+sDim.Render("now: "+p.File))
		}
	case dlDone:
		lines = append(lines, " "+sGood.Render("Finished. It's in your Library."))
	case dlCanceled:
		lines = append(lines, " "+sWarn.Render("Canceled. Download it again to resume where it stopped."))
	case dlFailed:
		lines = append(lines, " "+sBad.Render(err.Error()))
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

func downloadRow(tb table, d *download, sel bool) string {
	p, st, err := d.snapshot()
	name := []seg{t(d.repo, sPlain), t(":"+d.quant, sDim)}
	switch st {
	case dlRunning:
		frac := float64(p.Done) / float64(max(1, p.Total))
		n := int(min(1, frac)*16 + 0.5)
		eta := ""
		if p.Speed > 1 && p.Total > p.Done {
			eta = "  ETA " + units.Duration(time.Duration(float64(p.Total-p.Done)/p.Speed*float64(time.Second)))
		}
		return tb.row(sel, styled(fmt.Sprintf("↓ %d%%", pct(p)), sAccent), name, []seg{
			t(strings.Repeat("█", n), sAccent), t(strings.Repeat("░", 16-n), sDim),
			t(fmt.Sprintf("  %s / %s  %s/s%s", units.Bytes(p.Done), units.Bytes(p.Total), units.Bytes(int64(p.Speed)), eta), sDim)})
	case dlDone:
		return tb.row(sel, styled("✓ downloaded", sGood), name, dim(units.Bytes(p.Total)))
	case dlCanceled:
		return tb.row(sel, styled("canceled", sWarn), name, dim("start it again to resume where it stopped"))
	}
	return tb.row(sel, styled("✗ failed", sBad), name, styled(err.Error(), sBad))
}

func openBrowser(url string) { exec.Command("xdg-open", url).Start() }
