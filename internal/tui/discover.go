package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/gguf"
	"loom/internal/hf"
	"loom/internal/store"
	"loom/internal/units"
)

type discover struct {
	input   textinput.Model
	typingQ bool
	sc      scroller
	results []hf.Summary
	natural []hf.Summary
	sort    sorter
	shown   string
	loading bool
	err     error
	seq     int

	tasks []hf.Task
	task  hf.Task
	all   bool
}

type (
	searchMsg struct {
		seq   int
		query string
		res   []hf.Summary
		err   error
	}
	debounceMsg struct{ seq int }
	tasksMsg    struct{ tasks []hf.Task }
)

func newDiscover() *discover {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "model name, owner/repo or a huggingface.co link"
	return &discover{input: ti, loading: true}
}

func (d *discover) init(a *App) tea.Cmd {
	return tea.Batch(d.search(a), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		tasks, _ := a.hub.Tasks(ctx)
		return tasksMsg{tasks}
	})
}

func (d *discover) typing() bool { return d.typingQ || d.sort.picking }

func (d *discover) search(a *App) tea.Cmd {
	d.loading = true
	seq, q, task, all := d.seq, d.input.Value(), d.task.ID, d.all
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := a.hub.Search(ctx, q, task, all)
		return searchMsg{seq, q, res, err}
	}
}

var discoverSort = []sortCol{{"name", false}, {"params", true}, {"downloads", true}, {"likes", true}, {"updated", true}}

var discoverSortBy = map[string]func(x, y hf.Summary) int{
	"name":      func(x, y hf.Summary) int { return cmpText(x.ID, y.ID) },
	"params":    func(x, y hf.Summary) int { return cmpNum(x.GGUF.Params, y.GGUF.Params) },
	"downloads": func(x, y hf.Summary) int { return cmpNum(x.Downloads, y.Downloads) },
	"likes":     func(x, y hf.Summary) int { return cmpNum(x.Likes, y.Likes) },
	"updated":   func(x, y hf.Summary) int { return x.Updated.Compare(y.Updated) },
}

func (d *discover) hints(a *App) string {
	if d.typingQ {
		return a.hints("type", "search as you go", "confirm", "show results", "back", "stop typing")
	}
	if d.sort.picking {
		return d.sort.hints(a)
	}
	return a.hints("confirm", "quants & download", "search", "search", "sort", "sort", "filter", "task", "unsupported", "can't-run models", "help", "all keys")
}

func (d *discover) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tasksMsg:
		d.tasks = msg.tasks
	case searchMsg:
		if msg.seq != d.seq {
			return nil
		}
		d.loading, d.err, d.shown = false, msg.err, msg.query
		if msg.err == nil {
			d.natural = msg.res
			d.results = sortedBy(d.natural, d.sort, discoverSort, discoverSortBy)
			d.sc = scroller{}
		}
	case debounceMsg:
		if msg.seq == d.seq {
			return d.search(a)
		}
	case tea.KeyMsg:
		return d.key(a, msg)
	}
	return nil
}

func (d *discover) key(a *App, msg tea.KeyMsg) tea.Cmd {
	n := len(d.results)
	if d.sort.key(a, msg, discoverSort) {
		d.results = sortedBy(d.natural, d.sort, discoverSort, discoverSortBy)
		d.sc = scroller{}
		return nil
	}
	if d.typingQ {
		switch {
		case a.is(msg, "confirm"):
			d.typingQ = false
			d.input.Blur()
			if d.shown != d.input.Value() {
				d.seq++
				if _, _, ok := hf.ParseRef(d.input.Value()); ok {
					return d.open(a)
				}
				return d.search(a)
			}
		case a.is(msg, "back"):
			d.typingQ = false
			d.input.Blur()
		case msg.String() == "up":
			d.sc.move(-1, n)
		case msg.String() == "down":
			d.sc.move(1, n)
		default:
			before := d.input.Value()
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(msg)
			if d.input.Value() == before {
				return cmd
			}
			d.seq++
			seq := d.seq
			return tea.Batch(cmd, tea.Tick(350*time.Millisecond, func(time.Time) tea.Msg { return debounceMsg{seq} }))
		}
		return nil
	}
	switch {
	case a.is(msg, "up"):
		d.sc.move(-1, n)
	case a.is(msg, "down"):
		d.sc.move(1, n)
	case a.is(msg, "page-up"):
		d.sc.move(-10, n)
	case a.is(msg, "page-down"):
		d.sc.move(10, n)
	case a.is(msg, "top"):
		d.sc.cur = 0
	case a.is(msg, "bottom"):
		d.sc.cur = max(0, n-1)
	case a.is(msg, "sort"):
		d.sort.start(discoverSort)
		d.results = sortedBy(d.natural, d.sort, discoverSort, discoverSortBy)
		d.sc = scroller{}
	case a.is(msg, "search"):
		d.typingQ = true
		return d.input.Focus()
	case a.is(msg, "filter"):
		if len(d.tasks) == 0 {
			a.complain("The Hub's task list isn't available (offline?)")
			return nil
		}
		a.push(newFilterView(d.tasks, d.task))
	case a.is(msg, "unsupported"):
		d.all = !d.all
		d.seq++
		return d.search(a)
	case a.is(msg, "back"):
		if d.input.Value() != "" || d.task.ID != "" {
			d.input.SetValue("")
			d.task = hf.Task{}
			d.seq++
			return d.search(a)
		}
	case a.is(msg, "confirm"):
		return d.open(a)
	case a.is(msg, "open") && d.sc.cur < n:
		openBrowser("https://huggingface.co/" + d.results[d.sc.cur].ID)
	}
	return nil
}

func (d *discover) open(a *App) tea.Cmd {
	typed, _, typedOK := hf.ParseRef(d.input.Value())
	if d.sc.cur < len(d.results) && !(typedOK && d.shown != d.input.Value()) {
		if why := d.results[d.sc.cur].Runnable(); why != nil && !d.results[d.sc.cur].IsGGUF() {
			return d.findGGUF(a, d.results[d.sc.cur])
		}
	}
	id := ""
	switch {
	case typedOK && d.shown != d.input.Value():
		id = typed
	case d.sc.cur < len(d.results):
		id = d.results[d.sc.cur].ID
	case typedOK:
		id = typed
	default:
		return nil
	}
	rv := newRepoView(id)
	a.push(rv)
	return rv.load(a)
}

func (d *discover) view(a *App, w, h int) []string {
	search := sDim.Render(a.keys.Label("search")) + sDim.Render("  ") + d.input.View()
	if d.typingQ {
		d.input.Width = w - 14
		search = sAccent.Render("search") + sDim.Render("  ") + d.input.View()
	} else if d.input.Value() == "" {
		search = sDim.Render(a.keys.Label("search") + "  to search by name")
	}
	filter := sDim.Render(a.keys.Label("filter") + "  to filter by task (multimodal, audio, …)")
	if d.task.ID != "" {
		filter = sAccent.Render("filter") + "  " + sDim.Render(groupLabel(d.task.Group)+" › ") + sBold.Render(d.task.Label)
	}
	scope := sDim.Render(a.keys.Label("unsupported") + "  to also show models llama.cpp can't run")
	if d.all {
		scope = sAccent.Render("all models") + "  " + sDim.Render("including ones llama.cpp can't run, marked in red · "+a.keys.Label("unsupported")+" to hide them")
	}
	lines := []string{"", " " + fit(search, w-2), " " + filter, " " + scope, ""}

	switch {
	case d.err != nil:
		return append(lines, " "+sBad.Render("Search failed: "+d.err.Error()), " "+sDim.Render("Check your connection; you can still run what's in Library."))
	case d.loading && len(d.results) == 0:
		return append(lines, " "+spin(a.tick)+sDim.Render(" Searching Hugging Face…"))
	case len(d.results) == 0:
		msg := "No GGUF models found. Try fewer words, or the model's family name."
		if d.task.ID != "" {
			msg = "No " + d.task.Label + " GGUF models match. Broaden the search, or change the filter."
		}
		return append(lines, " "+sDim.Render(msg))
	}

	what := "GGUF models"
	if d.task.ID != "" {
		what = d.task.Label + " GGUF models"
	}
	title := "Trending " + what
	if d.shown != "" {
		title = fmt.Sprintf("%d %s matching “%s”, most downloaded first", len(d.results), what, d.shown)
	}
	if d.loading {
		title += "  " + spin(a.tick)
	}
	lines[4] = " " + sDim.Render(title)

	tb := newTable(w, hideColumns([]column{
		{id: "name", title: "  NAME"},
		{id: "params", title: "PARAMS", w: 8, right: true},
		{id: "downloads", title: "DOWNLOADS", w: 11, right: true},
		{id: "likes", title: "LIKES", w: 7, right: true},
		{id: "updated", title: "UPDATED", w: 9, min: 90},
		{id: "features", title: "FEATURES", w: 18, min: 100},
	}, func(id string) bool { return a.cfg.Hidden("discover", id) })...)
	tb = tb.sorted(d.sort, discoverSort)
	lines = append(lines, tb.header())
	from, to := d.sc.window(len(d.results), max(1, h-len(lines)))
	for i := from; i < to; i++ {
		r := d.results[i]
		owner, name, _ := strings.Cut(r.ID, "/")
		mark, nameStyle, num := t("  ", sPlain), sPlain, plain
		if downloaded(a.models, r.ID) {
			mark = t("✓ ", sGood)
		}
		feats := features(r)
		if why := r.Runnable(); why != nil {
			nameStyle, num = sDim, dim
			feats = styled(why.Short, sBad)
		}
		params := ""
		if r.GGUF.Params > 0 {
			params = gguf.Params(r.GGUF.Params)
		}
		lines = append(lines, tb.row(i == d.sc.cur,
			[]seg{mark, t(owner+"/", sDim), t(name, nameStyle)},
			num(params),
			num(units.Count(r.Downloads)),
			num(units.Count(r.Likes)),
			dim(units.Ago(r.Updated)),
			feats,
		))
	}
	return lines
}

func (d *discover) findGGUF(a *App, r hf.Summary) tea.Cmd {
	_, name, _ := strings.Cut(r.ID, "/")
	d.input.SetValue(name)
	d.all, d.task = false, hf.Task{}
	d.seq++
	a.say("%s isn't GGUF, looking for GGUF versions of it", name)
	return d.search(a)
}

func downloaded(models []store.Model, repo string) bool {
	for _, m := range models {
		if m.Repo == repo {
			return true
		}
	}
	return false
}

func features(r hf.Summary) []seg {
	var out []seg
	add := func(s seg) {
		if len(out) > 0 {
			out = append(out, t(" ", sPlain))
		}
		out = append(out, s)
	}
	if r.Pipeline == "image-text-to-text" || hasTag(r, "image-text-to-text") {
		add(t("vision", sAccent))
	}
	if hasTag(r, "moe") || hasTag(r, "mixture-of-experts") {
		add(t("MoE", sWarn))
	}
	if r.Gated {
		add(t("gated", sWarn))
	}
	if r.Pipeline == "feature-extraction" || r.Pipeline == "sentence-similarity" || hasTag(r, "sentence-transformers") {
		add(t("embedding", sFlag))
	}
	return out
}

func hasTag(r hf.Summary, tag string) bool {
	for _, x := range r.Tags {
		if x == tag {
			return true
		}
	}
	return false
}

func groupLabel(id string) string {
	for _, g := range hf.Groups {
		if g.ID == id {
			return g.Label
		}
	}
	return id
}
