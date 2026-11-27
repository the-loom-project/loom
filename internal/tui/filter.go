package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/hf"
)

type filterView struct {
	groups []hf.Group
	byID   map[string][]hf.Task
	cat    scroller
	task   scroller
	inTask bool
	pick   func(hf.Task)
}

func newFilterView(tasks []hf.Task, current hf.Task, pick func(hf.Task)) *filterView {
	f := &filterView{byID: map[string][]hf.Task{}, pick: pick}
	for _, t := range tasks {
		f.byID[t.Group] = append(f.byID[t.Group], t)
	}
	for _, g := range hf.Groups {
		if len(f.byID[g.ID]) > 0 {
			f.groups = append(f.groups, g)
		}
	}
	for i, g := range f.groups {
		for j, t := range f.byID[g.ID] {
			if current.ID != "" && t.ID == current.ID {
				f.cat.cur, f.task.cur, f.inTask = i+1, j, true
			}
		}
	}
	return f
}

func (f *filterView) tasks() []hf.Task {
	if f.cat.cur == 0 {
		return nil
	}
	return f.byID[f.groups[f.cat.cur-1].ID]
}

func (f *filterView) title(a *App) string { return "Filter by task" }

func (f *filterView) size(a *App) (int, int) { return 84, 20 }

func (f *filterView) hints(a *App) string {
	if f.inTask {
		return a.hints("confirm", "apply", "left", "categories", "back", "cancel")
	}
	return a.hints("right", "tasks", "confirm", "choose", "back", "cancel")
}

func (f *filterView) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	apply := func(t hf.Task) (popup, tea.Cmd) {
		f.pick(t)
		return nil, nil
	}
	move := func(d int) {
		if f.inTask {
			f.task.move(d, len(f.tasks()))
		} else {
			f.cat.move(d, len(f.groups)+1)
			f.task = scroller{}
		}
	}
	switch {
	case a.is(key, "back", "quit", "filter"):
		return nil, nil
	case a.is(key, "up"):
		move(-1)
	case a.is(key, "down"):
		move(1)
	case a.is(key, "left"):
		f.inTask = false
	case a.is(key, "right"):
		f.inTask = f.cat.cur > 0
	case a.is(key, "confirm"):
		switch {
		case f.cat.cur == 0:
			return apply(hf.Task{})
		case !f.inTask:
			f.inTask = true
		default:
			return apply(f.tasks()[f.task.cur])
		}
	}
	return f, nil
}

func (f *filterView) body(a *App, w, h int) []string {
	lw := 32
	left := []string{"All GGUF models"}
	for _, g := range f.groups {
		left = append(left, g.Label)
	}
	right := f.tasks()

	lines := []string{"", " " + sDim.Render(fit("CATEGORY", lw)) + "   " + sDim.Render("TASK"), ""}
	rows := max(1, h-len(lines))
	lf, lt := f.cat.window(len(left), rows)
	rf, rt := f.task.window(len(right), rows)
	for i := 0; i < rows; i++ {
		l, r := strings.Repeat(" ", lw), ""
		if li := lf + i; li < lt {
			l = f.cell(left[li], li == f.cat.cur, !f.inTask, lw)
		}
		if ri := rf + i; ri < rt {
			r = f.cell(right[ri].Label, ri == f.task.cur, f.inTask, w-lw-5)
		}
		lines = append(lines, " "+l+sEdge.Render(" │ ")+r)
	}
	if f.cat.cur == 0 {
		lines = append(lines, "", " "+sDim.Render("Pick a category to see its tasks."))
	}
	return lines
}

func (f *filterView) cell(text string, selected, focused bool, w int) string {
	text = fit(" "+text, w)
	switch {
	case selected && focused:
		return sPlain.Background(cSel).Bold(true).Render(text)
	case selected:
		return sDim.Render(text)
	}
	return text
}
