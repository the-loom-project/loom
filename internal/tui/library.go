package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	clipboard "loom/internal/clip"
	"loom/internal/gguf"
	"loom/internal/hw"
	"loom/internal/llama"
	"loom/internal/store"
	"loom/internal/units"
)

type library struct {
	sc        scroller
	query     textinput.Model
	searching bool
	kind      string
}

var libraryKinds = []option{
	{"", "Everything", "all downloaded models"},
	{"chat", "Chat", "models you can talk to"},
	{"vision", "Vision", "chat models that also read images"},
	{"embedding", "Embedding", "models that turn text into vectors"},
	{"fits", "Fits my GPU", "run comfortably on the GPU"},
	{"other", "Not runnable", "downloaded, but llama.cpp can't run them"},
}

func newLibrary() *library {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "filter by name"
	return &library{query: ti}
}

type entry struct {
	model *store.Model
	other *store.Other
}

func (l *library) entries(a *App) []entry {
	q := strings.ToLower(strings.TrimSpace(l.query.Value()))
	var out []entry
	for i := range a.models {
		m := &a.models[i]
		if !strings.Contains(strings.ToLower(m.Ref), q) || !l.kindMatches(a, m) {
			continue
		}
		out = append(out, entry{model: m})
	}
	if l.kind == "" || l.kind == "other" {
		for i := range a.others {
			if strings.Contains(strings.ToLower(a.others[i].Repo), q) {
				out = append(out, entry{other: &a.others[i]})
			}
		}
	}
	return out
}

func (l *library) kindMatches(a *App, m *store.Model) bool {
	switch l.kind {
	case "":
		return true
	case "other":
		return m.Kind() == gguf.NotLM
	case "chat":
		return m.Kind() == gguf.Chat
	case "vision":
		return m.Kind() == gguf.Chat && m.MMProj != ""
	case "embedding":
		return m.Kind() == gguf.Embedding
	case "fits":
		return m.Kind() != gguf.NotLM && gpuFit(a, m.Size) == hw.Fits
	}
	return false
}

func (l *library) selected(a *App) (entry, bool) {
	es := l.entries(a)
	if l.sc.cur >= len(es) {
		return entry{}, false
	}
	return es[l.sc.cur], true
}

func (l *library) typing() bool { return l.searching }

func (l *library) hints(a *App) string {
	if l.searching {
		return a.hints("confirm", "keep filter", "back", "clear")
	}
	return a.hints("confirm", "configure & run", "run", "run", "info", "details", "filter", "filter", "search", "search", "delete", "delete", "help", "all keys")
}

func (l *library) update(a *App, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if l.searching {
		switch {
		case a.is(key, "confirm"):
			l.searching = false
			l.query.Blur()
		case a.is(key, "back"):
			l.searching = false
			l.query.Blur()
			l.query.SetValue("")
			l.sc.cur = 0
		default:
			var cmd tea.Cmd
			l.query, cmd = l.query.Update(msg)
			l.sc.cur = 0
			return cmd
		}
		return nil
	}
	n := len(l.entries(a))
	e, hasSel := l.selected(a)
	switch {
	case a.is(key, "up"):
		l.sc.move(-1, n)
	case a.is(key, "down"):
		l.sc.move(1, n)
	case a.is(key, "page-up"):
		l.sc.move(-10, n)
	case a.is(key, "page-down"):
		l.sc.move(10, n)
	case a.is(key, "top"):
		l.sc.cur = 0
	case a.is(key, "bottom"):
		l.sc.cur = max(0, n-1)
	case a.is(key, "search"):
		l.searching = true
		return l.query.Focus()
	case a.is(key, "filter"):
		a.push(newPicker("Show", libraryKinds, l.kind, func(a *App, v string) tea.Cmd {
			l.kind, l.sc.cur = v, 0
			return nil
		}))
	case a.is(key, "back"):
		l.query.SetValue("")
		l.kind, l.sc.cur = "", 0
	case !hasSel:
	case a.is(key, "delete"):
		return l.askDelete(a, e)
	case e.other != nil:
		if a.is(key, "confirm", "run", "info") {
			a.complain("%s is not a GGUF model, so llama.cpp can't run it (x deletes it)", e.other.Repo)
		}
	case a.is(key, "info"):
		a.push(&infoView{model: *e.model})
	case a.is(key, "confirm", "run") && e.model.Kind() == gguf.NotLM:
		a.complain("%s isn't a language model, so llama.cpp can't run it (%s deletes it)", e.model.Name(), a.keys.Label("delete"))
	case a.is(key, "confirm"):
		return a.openSheet(*e.model, false)
	case a.is(key, "run"):
		return a.openSheet(*e.model, true)
	case a.is(key, "copy"), a.is(key, "copy-raw"):
		env := a.env(*e.model)
		a.copyCommand(env, a.profiles.Get(env), a.profiles.LastProfile(env.Model.Ref), a.is(key, "copy-raw"))
	}
	return nil
}

func (l *library) askDelete(a *App, e entry) tea.Cmd {
	if e.other != nil {
		o := *e.other
		a.push(&confirm{
			heading: "Delete " + o.Repo + "?",
			text:    fmt.Sprintf("Frees %s. llama.cpp can't run it, so loom can't use it.", units.Bytes(o.Size)),
			yes: func(a *App) tea.Cmd {
				if err := a.st.RemoveOther(o); err != nil {
					a.complain("%v", err)
				} else {
					a.say("Deleted %s", o.Repo)
				}
				return a.rescan()
			},
		})
		return nil
	}
	m := *e.model
	if a.isRunning(m.Ref) {
		a.complain("%s is running, stop it in Activity first", m.Name())
		return nil
	}
	a.push(&confirm{
		heading: "Delete " + m.Name() + "?",
		text:    fmt.Sprintf("%s (%s) is removed from disk. You can download it again from Discover.", m.Ref, units.Bytes(m.Size)),
		yes: func(a *App) tea.Cmd {
			if err := a.st.Remove(m); err != nil {
				a.complain("%v", err)
			} else {
				a.say("Deleted %s, freed %s", m.Ref, units.Bytes(m.Size))
			}
			return a.rescan()
		},
	})
	return nil
}

func (l *library) view(a *App, w, h int) []string {
	if !a.scanned {
		return []string{"", "  " + spin(a.tick) + sDim.Render(" Looking at your models…")}
	}
	if len(a.models)+len(a.others) == 0 {
		return []string{"", "", "  " + sBold.Render("No models yet."), "",
			"  Press " + sBold.Render(a.keys.Label("view-discover")) + " to browse Hugging Face and download one,",
			"  or put a .gguf file in " + sFlag.Render(home(a.st.LocalDir())) + ".", "",
			sDim.Render("  Tip: Q4_K_M is the usual quality/size sweet spot; loom marks what fits your GPU.")}
	}

	var lines []string
	if l.searching || l.query.Value() != "" || l.kind != "" {
		var f []string
		if l.kind != "" {
			for _, o := range libraryKinds {
				if o.value == l.kind {
					f = append(f, sAccent.Render(o.label))
				}
			}
		}
		if l.searching {
			l.query.Width = w - 6
			f = append(f, l.query.View())
		} else if l.query.Value() != "" {
			f = append(f, sDim.Render("“"+l.query.Value()+"”"))
		}
		lines = append(lines, " "+sDim.Render("Showing ")+strings.Join(f, sDim.Render(" · ")), "")
	}

	tb := newTable(w, hideColumns([]column{
		{title: "NAME"},
		{id: "params", title: "PARAMS", w: 7, right: true},
		{id: "quant", title: "QUANT", w: 10, min: 90},
		{id: "size", title: "SIZE", w: 8, right: true},
		{id: "context", title: "CTX", w: 6, right: true, min: 80},
		{id: "type", title: "TYPE", w: 12},
		{id: "gpu", title: "GPU", w: 14},
	}, func(id string) bool { return a.cfg.Hidden("library", id) })...)
	lines = append(lines, tb.header())
	es := l.entries(a)
	if len(es) == 0 {
		return append(lines, "", sDim.Render("  Nothing matches."))
	}
	from, to := l.sc.window(len(es), max(1, h-len(lines)))
	for i := from; i < to; i++ {
		lines = append(lines, l.row(a, tb, es[i], i == l.sc.cur))
	}
	return lines
}

func (l *library) row(a *App, tb table, e entry, sel bool) string {
	if e.other != nil {
		return tb.row(sel, styled(e.other.Repo, sDim), nil, nil, styled(units.Bytes(e.other.Size), sDim), nil, styled("not GGUF", sDim), styled("can't run", sDim))
	}
	m := e.model
	name := []seg{t(m.Name(), sPlain)}
	if m.Repo != "" {
		name = append(name, t("  "+m.Repo[:strings.Index(m.Repo, "/")], sDim))
	}
	if a.isRunning(m.Ref) {
		name = append(name, t("  ● running", sGood))
	}
	params, ctx := "", ""
	if m.Info != nil {
		params, ctx = m.Info.Size(), units.Tokens(m.Info.Context)
	}
	label := kindLabel(m.Kind(), m.MMProj != "")
	kind, gpu := dim(label), fitSeg(gpuFit(a, m.Size))
	switch m.Kind() {
	case gguf.Chat:
		kind = plain(label)
	case gguf.NotLM:
		kind, gpu = styled(label, sBad), nil
	}
	return tb.row(sel, name, plain(params), dim(m.Quant), plain(units.Bytes(m.Size)), dim(ctx), kind, gpu)
}

func (a *App) openSheet(m store.Model, runNow bool) tea.Cmd {
	if !a.hw.Installed {
		a.complain("llama.cpp isn't installed: sudo pacman -S llama-cpp ggml-vulkan")
		return nil
	}
	s := newSheet(a, m)
	if runNow {
		return s.run(a)
	}
	a.push(s)
	return nil
}

func (a *App) copyCommand(e llama.Env, c llama.Config, profile string, raw bool) {
	base := llama.Defaults(e).Fix(e)
	if p, ok := a.profiles.Profile(e.Model.Ref, profile); ok {
		base = p.Fix(e)
	}
	text, what := llama.Shell(c.LoomArgs(e, base, profile)), "loom command"
	if raw {
		text, what = llama.Build(e, c.Fix(e)).Line(), "llama.cpp command"
	}
	a.say("Copied the %s (%s)", what, clipboard.Copy(text))
}
