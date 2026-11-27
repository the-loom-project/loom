package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"loom/internal/hf"
)

type formRow struct {
	label string
	help  string
	text  bool
	get   func(f hf.Filters) string
	set   func(f *hf.Filters, v string)
}

var formRows = []formRow{
	{label: "Task", help: "What the model does, e.g. text generation or image-text-to-text. Enter picks one from the Hub's list."},
	{label: "Author", text: true, help: "Only models uploaded by this user or organization, e.g. unsloth or bartowski.",
		get: func(f hf.Filters) string { return f.Author }, set: func(f *hf.Filters, v string) { f.Author = v }},
	{label: "Min parameters", text: true, help: "At least this many parameters, e.g. 500M or 6B. A bare number means billions.",
		get: func(f hf.Filters) string { return f.MinParams }, set: func(f *hf.Filters, v string) { f.MinParams = v }},
	{label: "Max parameters", text: true, help: "At most this many parameters, e.g. 12B. Use it to keep to models that fit your GPU.",
		get: func(f hf.Filters) string { return f.MaxParams }, set: func(f *hf.Filters, v string) { f.MaxParams = v }},
	{label: "Tag", text: true, help: "Hub tags, separated by spaces or commas, a model needs all of them. Try moe, conversational, en, license:apache-2.0.",
		get: func(f hf.Filters) string { return f.Tag }, set: func(f *hf.Filters, v string) { f.Tag = v }},
	{label: "App", text: true, help: "Only models the Hub lists as usable with this app. Empty means any. Defaults to llama.cpp.",
		get: func(f hf.Filters) string { return f.App }, set: func(f *hf.Filters, v string) { f.App = v }},
	{label: "GGUF only", help: "Only models in the GGUF format, which is what llama.cpp runs. Turn it off to also browse models llama.cpp can't run."},
}

const (
	rowTask = 0
	rowApp  = 5
	rowGGUF = 6
)

type filterForm struct {
	f       hf.Filters
	orig    hf.Filters
	tasks   []hf.Task
	sc      scroller
	editing bool
	input   textinput.Model
	errText string
}

func newFilterForm(f hf.Filters, tasks []hf.Task) *filterForm {
	ti := textinput.New()
	ti.Prompt = ""
	return &filterForm{f: f, orig: f, tasks: tasks, input: ti}
}

func (v *filterForm) title(a *App) string { return "Filters" }

func (v *filterForm) size(a *App) (int, int) { return 100, 20 }

func (v *filterForm) hints(a *App) string {
	if v.editing {
		return a.hints("confirm", "accept", "back", "cancel")
	}
	return a.hints("confirm", "edit / toggle", "reset", "defaults", "back", "apply")
}

func (v *filterForm) value(row int) string {
	if row == rowTask {
		if t := v.f.Task; t.ID != "" {
			return groupLabel(t.Group) + " › " + t.Label
		}
		return ""
	}
	if row == rowGGUF {
		if v.f.All {
			return "[ ]"
		}
		return "[x]"
	}
	return formRows[row].get(v.f)
}

func (v *filterForm) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return v, nil
	}
	row := formRows[v.sc.cur]
	if v.editing {
		switch {
		case a.is(key, "confirm"):
			val := strings.TrimSpace(v.input.Value())
			if strings.HasSuffix(row.label, "parameters") {
				norm, err := hf.NormalizeParams(val)
				if err != nil {
					v.errText = "Needs " + err.Error()
					return v, nil
				}
				val = norm
			}
			row.set(&v.f, val)
			v.editing, v.errText = false, ""
		case a.is(key, "back"):
			v.editing, v.errText = false, ""
		default:
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			return v, cmd
		}
		return v, nil
	}
	switch {
	case a.is(key, "back", "quit", "filter"):
		if v.f == v.orig {
			return nil, nil
		}
		a.disc.f = v.f
		a.disc.seq++
		return nil, a.disc.search(a)
	case a.is(key, "up"):
		v.sc.move(-1, len(formRows))
	case a.is(key, "down"):
		v.sc.move(1, len(formRows))
	case a.is(key, "reset"):
		v.f = hf.DefaultFilters()
	case a.is(key, "confirm"), key.String() == " ", a.is(key, "edit") && row.text:
		switch {
		case v.sc.cur == rowGGUF:
			v.f.All = !v.f.All
		case v.sc.cur == rowTask:
			if len(v.tasks) == 0 {
				a.complain("The Hub's task list isn't available (offline?)")
				return v, nil
			}
			a.push(newFilterView(v.tasks, v.f.Task, func(t hf.Task) { v.f.Task = t }))
		default:
			v.editing = true
			v.input.SetValue(row.get(v.f))
			v.input.CursorEnd()
			return v, v.input.Focus()
		}
	}
	return v, nil
}

func (v *filterForm) body(a *App, w, h int) []string {
	const labelW = 18
	lines := []string{""}
	for i, row := range formRows {
		val := v.value(i)
		if val == "" && i != rowGGUF {
			val = sDim.Render("any")
			if i == rowApp {
				val = sDim.Render("any app")
			}
		}
		switch {
		case i == v.sc.cur && v.editing:
			lines = append(lines, " "+sBold.Render(fit(row.label, labelW))+" "+v.input.View())
		case i == v.sc.cur:
			plainVal := sPlain.Background(cSel).Bold(true).Render(fit(ansi.Strip(val), w-labelW-3))
			lines = append(lines, sPlain.Background(cSel).Bold(true).Render(" "+fit(row.label, labelW)+" ")+plainVal)
		default:
			lines = append(lines, " "+sDim.Render(fit(row.label, labelW))+" "+val)
		}
	}
	lines = append(lines, "", " "+heading(formRows[v.sc.cur].label, w-2))
	for _, l := range wrap(formRows[v.sc.cur].help, w-4) {
		lines = append(lines, "  "+sDim.Render(l))
	}
	if v.errText != "" {
		lines = append(lines, "", " "+sBad.Render(v.errText))
	}
	return lines
}
