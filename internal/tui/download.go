package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/hf"
	"loom/internal/units"
)

type dlState int

const (
	dlRunning dlState = iota
	dlDone
	dlFailed
	dlPaused
)

type download struct {
	repo, quant string
	started     time.Time
	cancel      context.CancelFunc
	hub, sha    string
	files       []hf.File
	meta        hf.Meta
	update      bool

	mu      sync.Mutex
	prog    hf.Progress
	st      dlState
	err     error
	removed bool
}

type (
	dlDoneMsg struct {
		d   *download
		err error
	}
	discardMsg struct {
		d   *download
		err error
	}
)

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

func (a *App) downloading(repo string) bool {
	return slices.ContainsFunc(a.downloads, func(d *download) bool { return d.repo == repo })
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
	if d := a.download(repo.ID, q.Name); d != nil {
		if d.active() {
			a.say("%s:%s is already downloading", repo.ID, q.Name)
			return nil
		}
		return a.resumeDownload(d)
	}
	files := append([]hf.File{}, q.Files...)
	if mm := repo.MMProj(); mm != nil {
		files = append(files, *mm)
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	d := &download{repo: repo.ID, quant: q.Name, started: time.Now(), hub: a.st.Hub(), sha: repo.SHA, files: files, meta: metaOf(repo), prog: hf.Progress{Total: total}}
	a.downloads = append(a.downloads, d)
	a.say("Downloading %s:%s, progress is in Library", repo.ID, q.Name)
	return a.runDownload(d)
}

func metaOf(r *hf.Repo) hf.Meta {
	return hf.Meta{Tags: r.Tags, Gated: r.Gated, Updated: r.Updated}
}

func (a *App) runDownload(d *download) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	d.mu.Lock()
	d.cancel, d.st, d.err, d.removed = cancel, dlRunning, nil, false
	d.mu.Unlock()
	return func() tea.Msg {
		return dlDoneMsg{d, a.hub.Download(ctx, d.hub, d.repo, d.sha, d.files, d.set)}
	}
}

func (a *App) resumeDownload(d *download) tea.Cmd {
	a.say("Resuming %s:%s", d.repo, d.quant)
	return a.runDownload(d)
}

func (a *App) pauseDownload(d *download) {
	d.cancel()
	a.say("Paused %s:%s, press %s to resume", d.repo, d.quant, a.keys.Label("pause"))
}

func (a *App) downloadDone(msg dlDoneMsg) tea.Cmd {
	d := msg.d
	d.mu.Lock()
	if d.removed {
		d.mu.Unlock()
		return nil
	}
	switch {
	case msg.err == nil:
		d.st = dlDone
		d.prog.Done = d.prog.Total
	case errors.Is(msg.err, context.Canceled):
		d.st = dlPaused
		d.prog.Speed = 0
	default:
		d.st, d.err = dlFailed, msg.err
	}
	st := d.st
	d.mu.Unlock()
	switch st {
	case dlDone:
		hf.WriteMeta(d.hub, d.repo, d.meta)
		if d.update {
			if err := hf.Prune(d.hub, d.repo, d.sha); err != nil {
				a.complain("Updated %s, but cleaning up the old version failed: %v", d.repo, err)
			} else {
				a.say("✓ Updated %s", d.repo)
			}
		} else {
			a.say("✓ Downloaded %s:%s, it's in your Library", d.repo, d.quant)
		}
		a.downloads = without(a.downloads, d)
	case dlFailed:
		a.complain("Download of %s:%s failed: %v", d.repo, d.quant, msg.err)
	}
	return a.rescan()
}

func (a *App) askDrop(d *download) {
	p, _, _ := d.snapshot()
	a.push(&confirm{
		heading: fmt.Sprintf("Cancel %s:%s?", d.repo, d.quant),
		text:    fmt.Sprintf("The %s downloaded so far is deleted.", units.Bytes(p.Done)),
		yes:     func(a *App) tea.Cmd { return a.dropDownload(d) },
	})
}

func (a *App) dropDownload(d *download) tea.Cmd {
	d.mu.Lock()
	d.removed = true
	d.mu.Unlock()
	d.cancel()
	a.downloads = without(a.downloads, d)
	alone := !a.downloading(d.repo)
	return func() tea.Msg {
		err := a.hub.Discard(d.hub, d.repo, d.files)
		if alone {
			err = errors.Join(err, hf.Clean(d.hub, d.repo))
		}
		return discardMsg{d, err}
	}
}

func (a *App) discarded(msg discardMsg) tea.Cmd {
	if msg.err != nil {
		a.complain("Cancelled %s:%s, but removing its files failed: %v", msg.d.repo, msg.d.quant, msg.err)
	} else {
		a.say("Cancelled %s:%s and removed its files", msg.d.repo, msg.d.quant)
	}
	return a.rescan()
}

// downloadRow is a download in the Library table.
func downloadRow(tb table, d *download, sel bool) string {
	p, st, err := d.snapshot()
	owner, repo, _ := strings.Cut(d.repo, "/")
	name := []seg{t(repo, sPlain)}
	size := dim(units.Bytes(p.Total))
	switch st {
	case dlRunning:
		frac := float64(p.Done) / float64(max(1, p.Total))
		n := int(min(1, frac)*14 + 0.5)
		info := ""
		if p.Speed > 1 {
			info = fmt.Sprintf("  %s/s", units.Bytes(int64(p.Speed)))
			if p.Total > p.Done {
				info += "  ETA " + units.Duration(time.Duration(float64(p.Total-p.Done)/p.Speed*float64(time.Second)))
			}
		}
		name = append(name, t(info, sDim))
		return tb.row(sel, dim(owner), name, nil, dim(d.quant), size, nil, styled(fmt.Sprintf("↓ %d%%", pct(p)), sAccent),
			[]seg{t(strings.Repeat("█", n), sAccent), t(strings.Repeat("░", 14-n), sDim)})
	case dlPaused:
		frac := float64(p.Done) / float64(max(1, p.Total))
		n := int(min(1, frac)*14 + 0.5)
		return tb.row(sel, dim(owner), name, nil, dim(d.quant), size, nil, styled(fmt.Sprintf("paused %d%%", pct(p)), sWarn),
			[]seg{t(strings.Repeat("█", n), sWarn), t(strings.Repeat("░", 14-n), sDim)})
	}
	return tb.row(sel, dim(owner), name, nil, dim(d.quant), size, nil, styled("✗ failed", sBad), styled(err.Error(), sBad))
}
