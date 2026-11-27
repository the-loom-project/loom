package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/gguf"
	"loom/internal/hf"
	"loom/internal/hw"
	"loom/internal/store"
	"loom/internal/units"
)

type repoView struct {
	id     string
	repo   *hf.Repo
	quants []hf.Quant
	rec    int
	err    error
	sc     scroller
}

type repoMsg struct {
	id   string
	repo *hf.Repo
	err  error
}

func newRepoView(id string) *repoView { return &repoView{id: id, rec: -1} }

func (r *repoView) load(a *App) tea.Cmd {
	id := r.id
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		repo, err := a.hub.Repo(ctx, id)
		return repoMsg{id, repo, err}
	}
}

func (r *repoView) title(a *App) string { return r.id }

func (r *repoView) hints(a *App) string {
	if r.repo == nil {
		return a.hints("back", "close")
	}
	return a.hints("confirm", "download", "open", "on huggingface.co", "back", "close")
}

func (r *repoView) size(a *App) (int, int) { return min(a.w-8, 116), min(a.h-6, 28) }

func (r *repoView) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	switch msg := msg.(type) {
	case repoMsg:
		if msg.id != r.id {
			return r, nil
		}
		r.repo, r.err = msg.repo, msg.err
		if msg.repo != nil {
			r.quants = msg.repo.Quants()
			names, sizes := make([]string, len(r.quants)), make([]int64, len(r.quants))
			for i, q := range r.quants {
				names[i], sizes[i] = q.Name, q.Size
			}
			r.rec = hw.Pick(names, sizes, a.hw.VRAM(""))
			r.sc.cur = max(0, r.rec)
		}
	case tea.KeyMsg:
		switch {
		case a.is(msg, "back", "quit"):
			return nil, nil
		case a.is(msg, "up"):
			r.sc.move(-1, len(r.quants))
		case a.is(msg, "down"):
			r.sc.move(1, len(r.quants))
		case a.is(msg, "page-up"):
			r.sc.move(-8, len(r.quants))
		case a.is(msg, "page-down"):
			r.sc.move(8, len(r.quants))
		case a.is(msg, "open"):
			openBrowser("https://huggingface.co/" + r.id)
		case a.is(msg, "confirm"):
			if r.repo != nil && r.repo.Runnable() != nil {
				a.complain("llama.cpp can't run this model, so there's nothing to download")
				return r, nil
			}
			if r.repo != nil && r.sc.cur < len(r.quants) {
				return r, a.startDownload(r.repo, r.quants[r.sc.cur])
			}
		}
	}
	return r, nil
}

func (r *repoView) body(a *App, w, h int) []string {
	switch {
	case r.err != nil:
		return []string{"", " " + sBad.Render(r.err.Error())}
	case r.repo == nil:
		return []string{"", " " + spin(a.tick) + sDim.Render(" Reading the repo…")}
	}
	repo := r.repo
	if why := repo.Runnable(); why != nil {
		lines := []string{"", " " + sBad.Render("llama.cpp can't run this model")}
		for _, l := range wrap(why.Long, w-2) {
			lines = append(lines, " "+l)
		}
		return append(lines, "", " "+sDim.Render("Nothing here can be downloaded. Close this and turn on GGUF only in the Discover filters ("+a.keys.Label("filter")+") to hide models like it."))
	}
	facts := []string{}
	if repo.Params > 0 {
		facts = append(facts, gguf.Params(repo.Params)+" parameters")
	}
	if repo.Arch != "" {
		facts = append(facts, repo.Arch)
	}
	if repo.Context > 0 {
		facts = append(facts, units.Tokens(repo.Context)+" context")
	}
	facts = append(facts, units.Count(repo.Downloads)+" downloads", units.Count(repo.Likes)+" likes")
	lines := []string{"", " " + sDim.Render(strings.Join(facts, " · "))}
	if repo.Gated {
		lines = append(lines, " "+sWarn.Render("Gated: accept the terms on huggingface.co and set HF_TOKEN or run hf auth login, or the download is refused."))
	}
	if mm := repo.MMProj(); mm != nil {
		lines = append(lines, " "+sDim.Render(fmt.Sprintf("Includes a vision adapter (%s) that downloads with any quant.", units.Bytes(mm.Size))))
	}
	if len(r.quants) == 0 {
		return append(lines, "", " "+sWarn.Render("This repo has no GGUF files, so llama.cpp can't run it."))
	}

	vram := a.hw.VRAM("")
	tb := newTable(w,
		column{title: "QUANT", w: 14},
		column{title: "SIZE", w: 8, right: true},
		column{title: "GPU", w: 14},
		column{title: "NOTES"},
	)
	lines = append(lines, "", tb.header())
	from, to := r.sc.window(len(r.quants), max(1, h-len(lines)-3))
	for i := from; i < to; i++ {
		q := r.quants[i]
		note := []seg{t(gguf.QuantNote(q.Name), sDim)}
		switch d := a.download(r.id, q.Name); {
		case store.Has(a.models, r.id, q.Name):
			note = append([]seg{t("✓ downloaded  ", sGood)}, note...)
		case d != nil && d.active():
			p, _, _ := d.snapshot()
			note = append([]seg{t(fmt.Sprintf("↓ %d%%  ", pct(p)), sAccent)}, note...)
		case i == r.rec:
			note = append([]seg{t("★ recommended  ", sAccent)}, note...)
		}
		lines = append(lines, tb.row(i == r.sc.cur, plain(q.Label()), plain(units.Bytes(q.Size)), fitSeg(hw.Assess(q.Size, vram, a.hw.RAM)), note))
	}

	q := r.quants[r.sc.cur]
	detail := fmt.Sprintf("%s · %s", q.Label(), units.Bytes(q.Size))
	if len(q.Files) > 1 {
		detail += fmt.Sprintf(" in %d files", len(q.Files))
	}
	lines = append(lines, "", " "+sDim.Render(detail+" → ")+sFlag.Render(home(a.st.Hub())))
	return lines
}
