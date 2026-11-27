package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/hf"
	"loom/internal/store"
)

type latestMsg struct {
	id   string
	repo *hf.Repo
}

func (a *App) checkLatest() tea.Cmd {
	var cmds []tea.Cmd
	for _, m := range a.models {
		if m.Source != store.HF || a.checked[m.Repo] {
			continue
		}
		a.checked[m.Repo] = true
		id := m.Repo
		cmds = append(cmds, func() tea.Msg {
			a.gate <- struct{}{}
			defer func() { <-a.gate }()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			repo, err := a.hub.Repo(ctx, id)
			if err != nil {
				repo = nil
			}
			return latestMsg{id, repo}
		})
	}
	return tea.Batch(cmds...)
}

func (a *App) latestArrived(msg latestMsg) {
	if msg.repo == nil {
		return
	}
	a.latest[msg.id] = msg.repo
	meta := metaOf(msg.repo)
	for i := range a.models {
		m := &a.models[i]
		if m.Repo != msg.id || (m.HasMeta && a.outdated(*m)) {
			continue
		}
		m.Meta, m.HasMeta = meta, true
	}
	for _, m := range a.models {
		if m.Repo == msg.id && !a.outdated(m) {
			hf.WriteMeta(a.st.Hub(), msg.id, meta)
			return
		}
	}
}

func (a *App) outdated(m store.Model) bool {
	r := a.latest[m.Repo]
	if r == nil || m.Source != store.HF {
		return false
	}
	files := map[string]string{}
	for _, f := range r.Files {
		files[f.Path] = f.BlobID
	}
	for path, blob := range m.Blobs {
		if files[path] != blob {
			return true
		}
	}
	return false
}

func (a *App) updateModel(m store.Model) tea.Cmd {
	for _, d := range a.downloads {
		if d.repo != m.Repo {
			continue
		}
		switch {
		case d.active():
			a.say("%s is already downloading, %s pauses it", m.Repo, a.keys.Label("pause"))
		case d.update:
			return a.resumeDownload(d)
		default:
			a.say("Finish or remove the download of %s:%s first", d.repo, d.quant)
		}
		return nil
	}
	r := a.latest[m.Repo]
	quants := map[string]hf.Quant{}
	for _, q := range r.Quants() {
		quants[q.Name] = q
	}
	var files []hf.File
	var names []string
	vision := false
	for _, x := range a.models {
		if x.Repo != m.Repo {
			continue
		}
		q, ok := quants[x.Quant]
		if !ok {
			a.complain("%s no longer has %s on the Hub, so it can't be updated", m.Repo, x.Quant)
			return nil
		}
		files = append(files, q.Files...)
		names = append(names, x.Quant)
		vision = vision || x.MMProj != ""
	}
	if mm := r.MMProj(); mm != nil && vision {
		files = append(files, *mm)
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	d := &download{repo: m.Repo, quant: strings.Join(names, ", "), started: time.Now(), hub: a.st.Hub(), sha: r.SHA, files: files, meta: metaOf(r), update: true, prog: hf.Progress{Total: total}}
	a.downloads = append(a.downloads, d)
	a.say("Updating %s, progress is in Library", m.Repo)
	return a.runDownload(d)
}
