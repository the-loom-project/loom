package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"loom/internal/gguf"
	"loom/internal/llama"
	"loom/internal/units"
)

type serverView struct{ inst *llama.Instance }

func (v *serverView) title(a *App) string { return v.inst.Model.Name() }

func (v *serverView) hints(a *App) string {
	h := []string{"delete", "stop", "copy", "copy loom cmd", "copy-raw", "copy llama cmd", "back", "close"}
	if v.inst.Config.Mode == llama.Serve {
		h = append([]string{"open", "open in browser"}, h...)
	}
	return a.hints(h...)
}

func (v *serverView) size(a *App) (int, int) { return min(a.w-8, 160), a.h - 4 }

func (v *serverView) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return v, nil
	}
	env := llama.Env{Model: v.inst.Model, HW: a.hw}
	switch {
	case a.is(key, "back", "quit", "details"):
		return nil, nil
	case a.is(key, "open"):
		if v.inst.Config.Mode == llama.Serve {
			openBrowser(v.inst.URL())
		}
	case a.is(key, "copy"), a.is(key, "copy-raw"):
		a.copyCommand(env, v.inst.Config, "", a.is(key, "copy-raw"))
	case a.is(key, "delete"):
		inst := v.inst
		if inst.Alive() {
			a.push(&confirm{heading: "Stop " + inst.Model.Name() + "?", text: fmt.Sprintf("The server on port %d shuts down.", inst.Port), yes: func(a *App) tea.Cmd { inst.Stop(); return nil }})
		}
	}
	return v, nil
}

func serverFacts(a *App, i *llama.Instance, w int) []string {
	st, err, _ := i.Snapshot(0)
	var badge string
	switch st {
	case llama.Loading:
		badge = spin(a.tick) + sWarn.Render(" loading the model…")
	case llama.Ready:
		badge = sGood.Render("● ready")
	case llama.Stopped:
		badge = sDim.Render("○ stopped")
	case llama.Failed:
		badge = sBad.Render("✗ failed: " + err.Error())
	}
	up := ""
	if st == llama.Loading || st == llama.Ready {
		up = sDim.Render("up " + units.Duration(time.Since(i.Started)))
	}
	env := llama.Env{Model: i.Model, HW: a.hw}
	label := func(l string) string { return " " + sDim.Render(fit(l, 9)) }
	lines := []string{between(" "+badge, up+" ", w), label("Runs as") + summary(env, i.Config)}
	switch {
	case i.Config.Mode == llama.Serve && i.Model.Kind() == gguf.Embedding:
		lines = append(lines, label("API")+sFlag.Render(i.URL()+"/v1/embeddings"))
	case i.Config.Mode == llama.Serve:
		lines = append(lines, label("Web UI")+sFlag.Render(i.URL()), label("API")+sFlag.Render(i.URL()+"/v1")+sDim.Render("  OpenAI-compatible"))
		if i.Config.Listen == "network" {
			lines = append(lines, label("")+sWarn.Render(fmt.Sprintf("Reachable from your network on port %d", i.Port)))
		}
	}
	if i.Config.Tools {
		lines = append(lines, label("")+sBad.Render("Tools are on: the model can read/write files and run commands in "+home(i.Command.Dir)))
	}
	return lines
}

func (v *serverView) body(a *App, w, h int) []string {
	i := v.inst
	lines := append([]string{""}, serverFacts(a, i, w)...)
	lines = append(lines, "", " "+heading("Command, flag by flag", w-2))
	lines = append(lines, renderCommand(i.Command, w-2, "", h-len(lines)-3, false)...)
	return append(lines, "", " "+sDim.Render("as one line  ")+clip(i.Command.Line(), w-16))
}

var logLine = regexp.MustCompile(`^\s*\d+\.\d+\.\d+\.\d+ ([IWEDV]) (.*)$`)

func parseLog(l string) (string, lipgloss.Style) {
	level := "I"
	if m := logLine.FindStringSubmatch(l); m != nil {
		level, l = m[1], m[2]
	}
	low := strings.ToLower(l)
	switch {
	case level == "E" || strings.Contains(low, "error") || strings.Contains(low, "failed") || strings.Contains(low, "cannot"):
		return l, sBad
	case level == "W" || strings.Contains(low, "warn"):
		return l, sWarn
	case strings.Contains(low, "listening") || strings.Contains(low, "model loaded"):
		return l, sGood
	case level == "D" || level == "V":
		return l, sDim
	}
	return l, sPlain
}
