package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/llama"
	"loom/internal/store"
)

type profilesView struct{ sc scroller }

func (v *profilesView) entries(a *App) []llama.Entry { return a.profiles.All() }

func (v *profilesView) typing() bool { return false }

func (v *profilesView) hints(a *App) string {
	return a.hints("run", "run", "confirm", "configure", "edit", "rename", "delete", "delete", "help", "all keys")
}

func (v *profilesView) update(a *App, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	all := v.entries(a)
	n := len(all)
	switch {
	case a.is(key, "up"):
		v.sc.move(-1, n)
	case a.is(key, "down"):
		v.sc.move(1, n)
	case a.is(key, "page-up"):
		v.sc.move(-10, n)
	case a.is(key, "page-down"):
		v.sc.move(10, n)
	case a.is(key, "top"):
		v.sc.cur = 0
	case a.is(key, "bottom"):
		v.sc.cur = max(0, n-1)
	case v.sc.cur >= n:
	case a.is(key, "confirm"):
		a.push(newEditSheet(a, all[v.sc.cur]))
	case a.is(key, "run"):
		e := all[v.sc.cur]
		m, installed := a.model(e.Ref)
		if !installed {
			a.complain("%s isn't downloaded", e.Ref)
			return nil
		}
		if !a.hw.Installed {
			a.complain("llama.cpp isn't installed: sudo pacman -S llama-cpp ggml-vulkan")
			return nil
		}
		return newSheetWith(a, m, e.Name).run(a)
	case a.is(key, "edit"):
		e := all[v.sc.cur]
		p, cmd := newNamePrompt("Rename "+e.Name, e.Name, func(a *App, name string) {
			if err := a.profiles.Rename(e.Ref, e.Name, name); err != nil {
				a.complain("%v", err)
			}
		})
		a.push(p)
		return cmd
	case a.is(key, "copy"), a.is(key, "copy-raw"):
		e := all[v.sc.cur]
		m, _ := a.model(e.Ref)
		env := a.env(m)
		cfg, _ := a.profiles.Profile(e.Ref, e.Name)
		a.copyCommand(env, cfg, e.Name, a.is(key, "copy-raw"))
	case a.is(key, "delete"):
		e := all[v.sc.cur]
		a.push(&confirm{heading: "Delete profile " + e.Name + "?", text: "The profile for " + e.Ref + " is removed. The model itself is kept.", yes: func(a *App) tea.Cmd {
			a.profiles.Delete(e.Ref, e.Name)
			return nil
		}})
	}
	return nil
}

func (v *profilesView) view(a *App, w, h int) []string {
	all := v.entries(a)
	if len(all) == 0 {
		return []string{"", "", "  " + sBold.Render("No profiles yet."), "",
			"  In the launch sheet from " + sBold.Render("Library") + ", press " + sBold.Render(a.keys.Label("save")) + " to save its settings as a profile."}
	}
	tb := newTable(w, column{id: "model", title: "MODEL", w: 40}, column{id: "profile", title: "PROFILE", w: 20}, column{title: "SETTINGS"}, column{title: "", w: 16, min: 110})
	lines := []string{"", tb.header()}
	from, to := v.sc.window(len(all), max(1, h-len(lines)))
	for i := from; i < to; i++ {
		e := all[i]
		cfg, _ := a.profiles.Profile(e.Ref, e.Name)
		m, installed := a.model(e.Ref)
		details := summary(a.env(m), cfg)
		if extra := strings.TrimSpace(cfg.Extra); extra != "" {
			details += " · " + extra
		}
		where := dim("")
		if !installed && a.scanned {
			where = styled("not downloaded", sWarn)
		}
		lines = append(lines, tb.row(i == v.sc.cur, plain(e.Ref), plain(e.Name), dim(details), where))
	}
	return lines
}

func (a *App) model(ref string) (store.Model, bool) {
	for _, m := range a.models {
		if m.Ref == ref {
			return m, true
		}
	}
	return store.Model{Ref: ref}, false
}
