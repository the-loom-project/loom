package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/hw"
	"loom/internal/keys"
	"loom/internal/store"
	"loom/internal/units"
)

type level int

const (
	good level = iota
	warn
	fail
)

type check struct {
	level  level
	name   string
	result string
	help   string
}

type env struct {
	tools     map[string]string
	diskFree  int64
	diskPath  string
	writeable bool
}

type envMsg env

var probedTools = []string{"llama-server", "llama-cli", "xdg-open", "wl-copy", "xclip", "xsel"}

func probeEnv(st *store.Store) tea.Cmd {
	return func() tea.Msg {
		e := env{tools: map[string]string{}}
		for _, name := range probedTools {
			if p, err := exec.LookPath(name); err == nil {
				e.tools[name] = p
			}
		}
		e.diskPath = st.Root
		dir := st.Root
		for dir != "/" && dir != "." {
			if _, err := os.Stat(dir); err == nil {
				break
			}
			dir = filepath.Dir(dir)
		}
		var s syscall.Statfs_t
		if syscall.Statfs(dir, &s) == nil {
			e.diskFree = int64(s.Bavail) * int64(s.Bsize)
		}
		if f, err := os.CreateTemp(dir, ".loom-doctor-*"); err == nil {
			e.writeable = true
			f.Close()
			os.Remove(f.Name())
		}
		return envMsg(e)
	}
}

type doctor struct {
	sc    scroller
	env   env
	known bool
}

func (v *doctor) typing() bool { return false }

func (v *doctor) hints(a *App) string {
	return a.hints("run", "check again", "help", "all keys")
}

func (v *doctor) update(a *App, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case envMsg:
		v.env, v.known = env(msg), true
	case tea.KeyMsg:
		n := len(v.checks(a))
		switch {
		case a.is(msg, "up"):
			v.sc.move(-1, n)
		case a.is(msg, "down"):
			v.sc.move(1, n)
		case a.is(msg, "top"):
			v.sc.cur = 0
		case a.is(msg, "page-up"):
			v.sc.move(-10, n)
		case a.is(msg, "page-down"):
			v.sc.move(10, n)
		case a.is(msg, "bottom"):
			v.sc.cur = max(0, n-1)
		case a.is(msg, "run"):
			a.say("Checking again…")
			return tea.Batch(
				func() tea.Msg { return hwMsg(hw.Detect()) },
				a.rescan(),
				probeEnv(a.st),
			)
		}
	}
	return nil
}

func (v *doctor) checks(a *App) []check {
	var out []check
	add := func(l level, name, result, help string) { out = append(out, check{l, name, result, help}) }

	switch {
	case !a.hwKnown:
		add(warn, "llama-server", "checking…", "")
	case !a.hw.Installed:
		add(fail, "llama-server", "not found", "Install llama.cpp: sudo pacman -S llama-cpp ggml-vulkan")
	case a.hw.Version == "":
		add(warn, "llama-server", home(v.env.tools["llama-server"]), "Found, but its version couldn't be read. Run `llama-server --version` to see why.")
	default:
		add(good, "llama-server", "llama.cpp "+a.hw.Version+"  "+home(v.env.tools["llama-server"]), "")
	}

	if v.known {
		if p, found := v.env.tools["llama-cli"]; found {
			add(good, "llama-cli", home(p), "")
		} else {
			add(warn, "llama-cli", "not found", "Terminal chat needs llama-cli, which ships with the llama.cpp package. Servers work without it.")
		}
	}

	if a.hwKnown && a.hw.Installed {
		if len(a.hw.Devices) == 0 {
			add(fail, "GPU", "none found", "llama.cpp sees no GPU, so everything runs on the CPU. Install a GPU backend (ggml-vulkan, or a ROCm/CUDA build) and check `llama-server --list-devices`.")
		}
		for i, d := range a.hw.Devices {
			name := "GPU"
			if len(a.hw.Devices) > 1 {
				name = fmt.Sprintf("GPU %d", i+1)
			}
			res := fmt.Sprintf("%s  %s, %s free", d.Short(), units.GiB(d.Total), units.GiB(d.Free))
			if d.Free < d.Total/4 {
				add(warn, name, res, "Most of the video memory is already taken, probably by another program or a running model. Models will spill into system RAM.")
			} else {
				add(good, name, res, "")
			}
		}
	}

	if a.hwKnown {
		switch {
		case a.hw.RAM == 0:
			add(warn, "Memory", "unknown", "Couldn't read /proc/meminfo.")
		case a.hw.RAM < 4*hw.GiB:
			add(warn, "Memory", units.GiB(a.hw.RAM)+" available", "Little free RAM: models that don't fit in video memory won't load.")
		default:
			add(good, "Memory", units.GiB(a.hw.RAM)+" available", "")
		}
	}

	if v.known {
		root := home(a.st.Root)
		switch {
		case !v.env.writeable:
			add(fail, "Models folder", root+"  not writable", "Downloads will fail. Fix permissions, or point $LOOM_MODELS somewhere you can write.")
		case v.env.diskFree < 10*hw.GiB:
			add(warn, "Models folder", fmt.Sprintf("%s  %s free", root, units.Bytes(v.env.diskFree)), "Little disk space left. A single model is often 5 to 40 GB.")
		default:
			add(good, "Models folder", fmt.Sprintf("%s  %s free", root, units.Bytes(v.env.diskFree)), "")
		}
	}

	if a.scanned {
		var broken []string
		for _, m := range a.models {
			if m.Err != nil {
				broken = append(broken, m.Ref)
			}
		}
		switch {
		case len(broken) > 0:
			add(warn, "Library", fmt.Sprintf("%d of %d models unreadable", len(broken), len(a.models)), "Can't read the GGUF header of: "+strings.Join(broken, ", ")+". The download may be damaged: delete it and download again.")
		default:
			add(good, "Library", fmt.Sprintf("%d models, %d other downloads", len(a.models), len(a.others)), "")
		}
	}

	if a.hub.HasToken() {
		add(good, "Hugging Face", "token found", "")
	} else {
		add(warn, "Hugging Face", "no token", "Fine for public models. Gated ones need HF_TOKEN, or `hf auth login`.")
	}

	if v.known {
		clipTool := ""
		for _, name := range []string{"wl-copy", "xclip", "xsel"} {
			if v.env.tools[name] != "" {
				clipTool = name
				break
			}
		}
		if clipTool != "" {
			add(good, "Clipboard", clipTool, "")
		} else {
			add(warn, "Clipboard", "no clipboard tool", "Copying commands falls back to the terminal's own clipboard support, which not every terminal has. Install wl-clipboard or xclip.")
		}
		if v.env.tools["xdg-open"] == "" {
			add(warn, "Browser", "xdg-open not found", "Opening a server in the browser won't work. Install xdg-utils.")
		} else {
			add(good, "Browser", "xdg-open found", "")
		}
	}

	if len(a.warnings) > 0 {
		add(warn, "Settings files", fmt.Sprintf("%d problem(s)", len(a.warnings)), strings.Join(a.warnings, " "))
	}
	if len(a.keys.Problems) > 0 {
		add(warn, "Key bindings", fmt.Sprintf("%d problem(s) in %s", len(a.keys.Problems), home(keys.Path())), strings.Join(a.keys.Problems, "; "))
	}
	return out
}

func (v *doctor) view(a *App, w, h int) []string {
	cs := v.checks(a)
	var bad, warns int
	for _, c := range cs {
		switch c.level {
		case fail:
			bad++
		case warn:
			warns++
		}
	}

	tb := newTable(w, column{title: "", w: 2}, column{title: "CHECK", w: 16}, column{title: "RESULT"})
	lines := []string{"", " " + doctorSummary(bad, warns), "", tb.header()}
	listH := min(len(cs), max(3, h-len(lines)-6))
	from, to := v.sc.window(len(cs), max(1, listH))
	for i := from; i < to; i++ {
		c := cs[i]
		mark, st := "✓", sGood
		switch c.level {
		case warn:
			mark, st = "!", sWarn
		case fail:
			mark, st = "✗", sBad
		}
		lines = append(lines, tb.row(i == v.sc.cur, styled(mark, st), plain(c.name), plain(c.result)))
	}

	if v.sc.cur < len(cs) {
		c := cs[v.sc.cur]
		lines = append(lines, "", sEdge.Render(strings.Repeat("─", w)))
		if c.help == "" {
			lines = append(lines, " "+sDim.Render("Nothing to do here."))
		}
		for _, l := range wrap(c.help, w-2) {
			if l != "" {
				lines = append(lines, " "+l)
			}
		}
	}
	return lines
}

func doctorSummary(bad, warns int) string {
	switch {
	case bad > 0:
		return sBad.Render(fmt.Sprintf("%d problem(s)", bad)) + sDim.Render(fmt.Sprintf(" and %d warning(s). Select a row to see what to do.", warns))
	case warns > 0:
		return sWarn.Render(fmt.Sprintf("%d warning(s).", warns)) + sDim.Render(" Everything essential works. Select a row for details.")
	}
	return sGood.Render("Everything looks good.")
}
