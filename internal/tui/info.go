package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/llama"
	"loom/internal/store"
	"loom/internal/units"
)

type infoView struct {
	model store.Model
	sc    scroller
}

func (v *infoView) title(a *App) string { return v.model.Name() }

func (v *infoView) hints(a *App) string {
	return a.hints("confirm", "configure & run", "copy", "copy loom cmd", "copy-raw", "copy llama cmd", "back", "close")
}

func (v *infoView) size(a *App) (int, int) {
	w := min(a.w-8, 120)
	return w, min(len(v.body(a, w-4, 100))+2, a.h-6)
}

func (v *infoView) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return v, nil
	}
	switch {
	case a.is(key, "back", "quit", "info"):
		return nil, nil
	case a.is(key, "up"):
		v.sc.move(-1, 100)
	case a.is(key, "down"):
		v.sc.move(1, 100)
	case a.is(key, "copy"), a.is(key, "copy-raw"):
		env := a.env(v.model)
		a.copyCommand(env, a.profiles.Get(env), a.profiles.LastProfile(env.Model.Ref), a.is(key, "copy-raw"))
	case a.is(key, "confirm"):
		m := v.model
		return nil, a.openSheet(m, false)
	}
	return v, nil
}

func (v *infoView) body(a *App, w, h int) []string {
	m := v.model
	env := a.env(m)
	cfg := a.profiles.Get(env)
	mem := llama.Estimate(env, cfg)

	row := func(label, value string) string { return " " + sDim.Render(fit(label, 14)) + value }
	lines := []string{"", " " + sDim.Render(m.Ref), ""}
	if m.Info != nil {
		i := m.Info
		lines = append(lines, " "+heading("Model", w-2))
		lines = append(lines, row("Parameters", i.Size()))
		lines = append(lines, row("Architecture", i.Arch))
		if i.Blocks > 0 {
			shape := fmt.Sprintf("%d layers", i.Blocks)
			if i.EmbedDim > 0 {
				shape += fmt.Sprintf(" · width %d", i.EmbedDim)
			}
			if i.Heads > 0 {
				shape += fmt.Sprintf(" · %d heads", i.Heads)
				if i.HeadsKV > 0 && i.HeadsKV != i.Heads {
					shape += fmt.Sprintf(" (%d for the cache)", i.HeadsKV)
				}
			}
			lines = append(lines, row("Shape", shape))
		}
		lines = append(lines, row("Context", units.Tokens(i.Context)+" tokens at most"))
		if i.Experts > 0 {
			lines = append(lines, row("Experts", fmt.Sprintf("mixture of %d (only a few active per token)", i.Experts)))
		}
		lines = append(lines, row("Use", kindLabel(m.Kind(), false)+map[bool]string{true: ", reads images", false: ""}[m.MMProj != ""]))
	} else if m.Err != nil {
		lines = append(lines, " "+sBad.Render("The GGUF header couldn't be read: "+m.Err.Error()))
	}
	lines = append(lines, "", " "+heading("Files", w-2))
	lines = append(lines, row("Weights", units.Bytes(m.Size)+"  "+sFlag.Render(home(m.Path))))
	if m.MMProj != "" {
		lines = append(lines, row("Vision adapter", sFlag.Render(home(m.MMProj))))
	}
	lines = append(lines, "", " "+heading("When you run it", w-2))
	lines = append(lines, row("Settings", summary(env, cfg)))
	mm := units.Bytes(mem.Weights) + " weights"
	if mem.Known {
		mm += fmt.Sprintf(" + %s for %s of context", units.Bytes(mem.KV), units.Tokens(uint64(mem.Context)))
		if mem.Floor {
			mm += " (at least)"
		}
	}
	if mem.GPU > 0 {
		mm += fmt.Sprintf("  of %s GPU memory", units.VRAM(mem.GPU))
	}
	lines = append(lines, row("Memory", mm))
	lines = append(lines, row("Fit", strings.TrimSpace(plainText(fitSeg(gpuFit(a, m.Size))))))
	return lines
}

func plainText(segs []seg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.style.Render(s.text))
	}
	return b.String()
}
