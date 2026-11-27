package tui

import (
	"context"
	"fmt"
	"slices"
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
	shown   string
	loading bool
	err     error
	seq     int

	tasks []hf.Task
	f     hf.Filters
	urls  []string
	page  int

	sizes    map[string]sizeRange
	fetching map[string]bool
	gate     chan struct{}
	winFrom  int
	winTo    int
}

type sizeRange struct{ min, max int64 }

type (
	searchMsg struct {
		seq   int
		query string
		res   []hf.Summary
		next  string
		err   error
	}
	debounceMsg struct{ seq int }
	tasksMsg    struct{ tasks []hf.Task }
	sizeMsg     struct {
		id string
		sizeRange
	}
)

func newDiscover() *discover {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "model name, owner/repo or a huggingface.co link"
	return &discover{input: ti, loading: true, f: hf.DefaultFilters(), sizes: map[string]sizeRange{}, fetching: map[string]bool{}, gate: make(chan struct{}, 4)}
}

func (d *discover) init(a *App) tea.Cmd {
	return tea.Batch(d.search(a), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		tasks, _ := a.hub.Tasks(ctx)
		return tasksMsg{tasks}
	})
}

func (d *discover) typing() bool { return d.typingQ }

func (d *discover) search(a *App) tea.Cmd {
	d.urls, d.page = []string{""}, 0
	return d.fetch(a)
}

func (d *discover) fetch(a *App) tea.Cmd {
	d.loading = true
	seq, q, f, url := d.seq, d.input.Value(), d.f, d.urls[d.page]
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, next, err := a.hub.Search(ctx, q, f, url)
		return searchMsg{seq, q, res, next, err}
	}
}

func (d *discover) turn(a *App, delta int) tea.Cmd {
	if page := d.page + delta; page < 0 || page >= len(d.urls) {
		return nil
	}
	d.page += delta
	d.seq++
	return d.fetch(a)
}

func (d *discover) hints(a *App) string {
	if d.typingQ {
		return a.hints("type", "search as you go", "confirm", "show results", "back", "stop typing")
	}
	return a.hints("confirm", "quants & download", "search", "search", "next-page", "next page", "prev-page", "previous", "sort", "sort by", "sort-flip", "flip order", "filter", "filters", "help", "all keys")
}

func (d *discover) update(a *App, msg tea.Msg) tea.Cmd {
	cmd := d.handle(a, msg)
	switch msg.(type) {
	case searchMsg, tea.KeyMsg:
		return tea.Batch(cmd, d.fetchSizes(a))
	}
	return cmd
}

// fetchSizes looks up the quantization sizes of the rows on screen, a few at a time.
func (d *discover) fetchSizes(a *App) tea.Cmd {
	if a.tab != tabDiscover {
		return nil
	}
	var cmds []tea.Cmd
	for i := d.winFrom; i < min(d.winTo, len(d.results)); i++ {
		id := d.results[i].ID
		if _, known := d.sizes[id]; known || d.fetching[id] || !d.results[i].IsGGUF() {
			continue
		}
		d.fetching[id] = true
		cmds = append(cmds, func() tea.Msg {
			d.gate <- struct{}{}
			defer func() { <-d.gate }()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			repo, err := a.hub.Repo(ctx, id)
			var r sizeRange
			if err == nil {
				for _, q := range repo.Quants() {
					if r.min == 0 || q.Size < r.min {
						r.min = q.Size
					}
					r.max = max(r.max, q.Size)
				}
			}
			return sizeMsg{id, r}
		})
	}
	return tea.Batch(cmds...)
}

func (d *discover) handle(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case sizeMsg:
		delete(d.fetching, msg.id)
		d.sizes[msg.id] = msg.sizeRange
	case tasksMsg:
		d.tasks = msg.tasks
	case searchMsg:
		if msg.seq != d.seq {
			return nil
		}
		d.loading, d.err, d.shown = false, msg.err, msg.query
		if msg.err == nil {
			d.urls = d.urls[:d.page+1]
			if msg.next != "" {
				d.urls = append(d.urls, msg.next)
			}
			for i := range msg.res {
				slices.Sort(msg.res[i].Tags)
			}
			d.results = msg.res
			d.sc = scroller{}
			d.winFrom, d.winTo = 0, 25
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

// filterSummary describes the filters that differ from the defaults.
func filterSummary(f hf.Filters) []string {
	var parts []string
	if f.Task.ID != "" {
		parts = append(parts, sDim.Render(groupLabel(f.Task.Group)+" › ")+sBold.Render(f.Task.Label))
	}
	if f.Author != "" {
		parts = append(parts, sDim.Render("by ")+sBold.Render(f.Author))
	}
	switch {
	case f.MinParams != "" && f.MaxParams != "":
		parts = append(parts, sBold.Render(f.MinParams+"–"+f.MaxParams)+sDim.Render(" parameters"))
	case f.MinParams != "":
		parts = append(parts, sBold.Render("≥"+f.MinParams)+sDim.Render(" parameters"))
	case f.MaxParams != "":
		parts = append(parts, sBold.Render("≤"+f.MaxParams)+sDim.Render(" parameters"))
	}
	if f.Tag != "" {
		parts = append(parts, sDim.Render("tag ")+sBold.Render(f.Tag))
	}
	switch app := f.App; {
	case app == "":
		parts = append(parts, sDim.Render("any app"))
	case app != hf.DefaultFilters().App:
		parts = append(parts, sDim.Render("app ")+sBold.Render(app))
	}
	if f.All {
		parts = append(parts, sBold.Render("including non-GGUF")+sDim.Render(" (marked in red if llama.cpp can't run them)"))
	}
	return parts
}

func (d *discover) key(a *App, msg tea.KeyMsg) tea.Cmd {
	n := len(d.results)
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
	case a.is(msg, "next-page"):
		return d.turn(a, 1)
	case a.is(msg, "prev-page"):
		return d.turn(a, -1)
	case a.is(msg, "sort"):
		cur := hf.SortByID(d.f.Sort)
		var opts []option
		for _, k := range hf.Sorts {
			help := k.Order(k.AscFirst)
			if k.ID == cur.ID {
				help = k.Order(d.f.Asc)
			}
			opts = append(opts, option{k.ID, k.Label, help})
		}
		a.push(newPicker("Sort by", opts, cur.ID, func(a *App, id string) tea.Cmd {
			if id == cur.ID {
				return nil
			}
			d.f.Sort, d.f.Asc = id, hf.SortByID(id).AscFirst
			d.seq++
			return d.search(a)
		}))
	case a.is(msg, "sort-flip"):
		cur := hf.SortByID(d.f.Sort)
		if cur.Asc == "" {
			a.say("The Hub only sorts %s from most to least", strings.ToLower(cur.Label))
			return nil
		}
		d.f.Asc = !d.f.Asc
		d.seq++
		return d.search(a)
	case a.is(msg, "search"):
		d.typingQ = true
		return d.input.Focus()
	case a.is(msg, "filter"):
		if len(d.tasks) == 0 {
			a.complain("The Hub's task list isn't available (offline?)")
			return nil
		}
		a.push(newFilterForm(d.f, d.tasks))
	case a.is(msg, "back"):
		if d.input.Value() != "" || d.f != hf.DefaultFilters() {
			d.input.SetValue("")
			d.f = hf.DefaultFilters()
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

// find searches for a repo and opens its page, e.g. from a model in the Library.
func (d *discover) find(a *App, repo string) tea.Cmd {
	_, name, _ := strings.Cut(repo, "/")
	d.input.SetValue(name)
	d.f = hf.Filters{}
	d.seq++
	a.tab = tabDiscover
	rv := newRepoView(repo)
	a.push(rv)
	return tea.Batch(d.search(a), rv.load(a))
}

func (d *discover) open(a *App) tea.Cmd {
	typed, _, typedOK := hf.ParseRef(d.input.Value())
	if d.sc.cur < len(d.results) && !(typedOK && d.shown != d.input.Value()) {
		if r := d.results[d.sc.cur]; !r.IsGGUF() {
			return d.findGGUF(a, r)
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
	lines := []string{""}
	if line, ok := searchLine(&d.input, d.typingQ, w); ok {
		lines = append(lines, line)
	}
	if parts := filterSummary(d.f); len(parts) > 0 {
		lines = append(lines, filterLine(strings.Join(parts, sDim.Render(" · "))))
	}
	lines = append(lines, "")
	titleRow := len(lines) - 1

	switch {
	case d.err != nil:
		return append(lines, " "+sBad.Render("Search failed: "+d.err.Error()), " "+sDim.Render("Check your connection; you can still run what's in Library."))
	case d.loading && len(d.results) == 0:
		return append(lines, " "+spin(a.tick)+sDim.Render(" Searching Hugging Face…"))
	case len(d.results) == 0:
		msg := "No GGUF models found. Try fewer words, or the model's family name."
		if d.f != hf.DefaultFilters() {
			msg = "No models match. Broaden the search, or change the filters (" + a.keys.Label("filter") + ")."
		}
		return append(lines, " "+sDim.Render(msg))
	}

	what := "GGUF models"
	if d.f.All {
		what = "models"
	}
	if d.f.Task.ID != "" {
		what = d.f.Task.Label + " " + what
	}
	title := what
	if d.shown != "" {
		title += fmt.Sprintf(" matching “%s”", d.shown)
	}
	title += fmt.Sprintf(" · %s · page %d", hf.SortByID(d.f.Sort).Order(d.f.Asc), d.page+1)
	if d.loading {
		title += "  " + spin(a.tick)
	}
	lines[titleRow] = " " + sDim.Render(title)

	tb := newTable(w, hideColumns([]column{
		{id: "author", title: "  AUTHOR", w: 20},
		{id: "name", title: "NAME"},
		{id: "params", title: "PARAMS", w: 8, right: true},
		{id: "size", title: "SIZE", w: 15, right: true, min: 80},
		{id: "downloads", title: "DOWNLOADS", w: 11, right: true},
		{id: "likes", title: "LIKES", w: 7, right: true},
		{id: "updated", title: "UPDATED", w: 9, min: 90},
	}, func(id string) bool { return a.cfg.Hidden("discover", id) })...)
	lines = append(lines, tb.header())
	from, to := d.sc.window(len(d.results), max(1, h-len(lines)-tagRows))
	d.winFrom, d.winTo = from, to
	for i := from; i < to; i++ {
		r := d.results[i]
		owner, name, _ := strings.Cut(r.ID, "/")
		mark, nameStyle, num := t("  ", sPlain), sPlain, plain
		if downloaded(a.models, r.ID) {
			mark = t("✓ ", sGood)
		}
		if r.Runnable() != nil {
			nameStyle, num = sDim, dim
		}
		params := ""
		if r.GGUF.Params > 0 {
			params = gguf.Params(r.GGUF.Params)
		}
		size := d.sizeCell(a, r.ID, r.Runnable() != nil)
		label := []seg{t(name, nameStyle)}
		if r.Gated {
			label = append(label, t(" "+lockGlyph, sWarn))
		}
		lines = append(lines, tb.row(i == d.sc.cur,
			[]seg{mark, t(owner, sDim)},
			label,
			num(params),
			size,
			num(units.Count(r.Downloads)),
			num(units.Count(r.Likes)),
			dim(units.Ago(r.Updated)),
		))
	}
	for len(lines) < h-tagRows {
		lines = append(lines, "")
	}
	var tags []string
	if d.sc.cur < len(d.results) {
		tags = d.results[d.sc.cur].Tags
	}
	return append(lines, tagBlock(a, tags, w)...)
}

func (d *discover) findGGUF(a *App, r hf.Summary) tea.Cmd {
	_, name, _ := strings.Cut(r.ID, "/")
	d.input.SetValue(name)
	d.f = hf.Filters{}
	d.seq++
	a.say("%s isn't GGUF, looking for GGUF versions of it", name)
	return d.search(a)
}

func (d *discover) sizeCell(a *App, id string, dimmed bool) []seg {
	if d.fetching[id] {
		return dim("…")
	}
	s := d.sizes[id]
	if s.max == 0 {
		return nil
	}
	paint := func(n int64) seg {
		st := a.fitColor(n)
		if dimmed {
			st = sDim
		}
		return t(fmt.Sprintf("%6s", compactBytes(n)), st)
	}
	if s.min == s.max {
		return []seg{paint(s.max)}
	}
	return []seg{paint(s.min), t(" – ", sDim), paint(s.max)}
}

func compactBytes(n int64) string {
	if n >= 1e10 {
		return fmt.Sprintf("%.0f GB", float64(n)/1e9)
	}
	return units.Bytes(n)
}

func downloaded(models []store.Model, repo string) bool {
	for _, m := range models {
		if m.Repo == repo {
			return true
		}
	}
	return false
}

const lockGlyph = ""

func groupLabel(id string) string {
	for _, g := range hf.Groups {
		if g.ID == id {
			return g.Label
		}
	}
	return id
}
