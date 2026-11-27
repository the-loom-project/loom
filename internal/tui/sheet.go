package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/hw"
	"loom/internal/llama"
	"loom/internal/store"
	"loom/internal/units"
)

type sheet struct {
	env     llama.Env
	cfg     llama.Config
	sel     int
	editing bool
	input   textinput.Model
	errText string
	saving  bool
	profile string
	off     int
}

type chatDoneMsg struct{ err error }

type editorDoneMsg struct {
	s    *sheet
	path string
	base []string
	err  error
}

// flagLines is the launch command as one flag and its value per line. The
// lines that come from the settings, as opposed to the extra flags, are base.
func (s *sheet) flagLines() (lines, base []string) {
	for _, arg := range llama.Build(s.env, s.cfg).Args {
		line := llama.Shell(arg.Words())
		lines = append(lines, line)
		if arg.Setting != "extra" {
			base = append(base, line)
		}
	}
	return lines, base
}

// editFlags opens the flags llama.cpp will get in the user's editor, in this terminal.
func (s *sheet) editFlags(a *App) tea.Cmd {
	f, err := os.CreateTemp("", "loom-flags-*.txt")
	if err != nil {
		a.complain("%v", err)
		return nil
	}
	lines, base := s.flagLines()
	_, err = f.WriteString(strings.Join(lines, "\n") + "\n")
	if a.failed(errors.Join(err, f.Close()), "Couldn't write the flags file") {
		return nil
	}
	argv := a.cfg.EditorCmd()
	cmd := exec.Command(argv[0], append(argv[1:], f.Name())...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{s, f.Name(), base, err} })
}

// flagsEdited keeps every line that isn't one of the settings' own flags as
// an extra flag. llama.cpp lets a later flag win, so changing a line from the
// settings overrides it.
func (s *sheet) flagsEdited(a *App, m editorDoneMsg) {
	data, err := os.ReadFile(m.path)
	os.Remove(m.path)
	if m.err != nil {
		a.complain("Couldn't run the editor (%s): %v", a.cfg.EditorCmd()[0], m.err)
		return
	}
	if err != nil {
		a.complain("%v", err)
		return
	}
	own := map[string]int{}
	for _, line := range m.base {
		own[line]++
	}
	var extra llama.Args
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case own[line] > 0:
			own[line]--
		default:
			words, err := llama.SplitArgs(line)
			if err != nil {
				a.complain("The flags weren't changed: %s needs balanced quotes", line)
				return
			}
			extra = append(extra, words...)
		}
	}
	s.cfg.Extra = extra
}

func newSheet(a *App, m store.Model) *sheet {
	env := a.env(m)
	ti := textinput.New()
	ti.Prompt = ""
	return &sheet{env: env, cfg: a.profiles.Get(env), input: ti, profile: a.profiles.LastProfile(m.Ref)}
}

func newSheetWith(a *App, m store.Model, profile string) *sheet {
	s := newSheet(a, m)
	s.load(a, profile)
	return s
}

// load switches to a saved profile, or to the defaults for "".
func (s *sheet) load(a *App, profile string) {
	s.profile = profile
	s.cfg = llama.Defaults().Fix(s.env)
	if c, ok := a.profiles.Profile(s.env.Model.Ref, profile); ok {
		s.cfg = c.Fix(s.env)
	}
}

func newEditSheet(a *App, e llama.Entry) *sheet {
	m, _ := a.model(e.Ref)
	env := a.env(m)
	ti := textinput.New()
	ti.Prompt = ""
	cfg, _ := a.profiles.Profile(e.Ref, e.Name)
	if m.Path != "" {
		cfg = cfg.Fix(env)
	}
	return &sheet{env: env, cfg: cfg, input: ti, saving: true, profile: e.Name}
}

func (s *sheet) settings() []llama.Setting {
	list := llama.Settings(s.env, s.cfg)
	s.sel = min(s.sel, len(list)-1)
	return list
}

func (s *sheet) title(a *App) string {
	if s.saving {
		return "Profile " + s.profile + " for " + s.env.Model.Name()
	}
	return "Run " + s.env.Model.Name()
}

func (s *sheet) hints(a *App) string {
	if s.editing {
		return a.hints("confirm", "accept", "back", "cancel")
	}
	if s.saving {
		return a.hints("save", "save", "left", "change", "editor", "flags in editor", "reset", "defaults", "back", "discard")
	}
	return a.hints("run", "run", "left", "change", "editor", "flags in editor", "profile", "profile", "save", "save profile", "reset", "defaults", "copy", "copy command", "back", "close")
}

func (s *sheet) size(a *App) (int, int) { return min(a.w-8, 170), min(a.h-6, 46) }

func (s *sheet) update(a *App, msg tea.Msg) (popup, tea.Cmd) {
	if m, ok := msg.(editorDoneMsg); ok && m.s == s {
		s.flagsEdited(a, m)
		return s, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	list := s.settings()
	cur := list[s.sel]
	if s.editing {
		switch {
		case a.is(key, "confirm"):
			if err := cur.Set(&s.cfg, s.input.Value()); err != nil {
				s.errText = "Needs " + err.Error()
				return s, nil
			}
			s.editing, s.errText = false, ""
		case a.is(key, "back"):
			s.editing, s.errText = false, ""
		default:
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			return s, cmd
		}
		return s, nil
	}
	switch {
	case a.is(key, "back", "quit", "view-library"):
		return nil, nil
	case a.is(key, "up"):
		s.sel = max(0, s.sel-1)
	case a.is(key, "down"):
		s.sel = min(len(list)-1, s.sel+1)
	case a.is(key, "left"):
		if !cur.Text {
			cur.Cycle(s.env, &s.cfg, -1)
		}
	case a.is(key, "right"):
		if cur.Text {
			return s, s.edit(cur)
		}
		cur.Cycle(s.env, &s.cfg, 1)
	case a.is(key, "edit") && cur.Text:
		return s, s.edit(cur)
	case s.saving && (a.is(key, "save") || a.is(key, "confirm") && !cur.Text):
		if !a.failed(a.profiles.Save(s.env.Model.Ref, s.profile, s.cfg), "Couldn't save the profile") {
			a.say("Saved profile %s", s.profile)
		}
		return nil, nil
	case s.saving && a.is(key, "run", "copy", "profile"):
	case a.is(key, "editor"):
		return s, s.editFlags(a)
	case a.is(key, "save"):
		p, cmd := newNamePrompt("Save as profile", s.profile, func(a *App, name string) {
			_, existed := a.profiles.Profile(s.env.Model.Ref, name)
			if a.failed(a.profiles.Save(s.env.Model.Ref, name, s.cfg), "Couldn't save the profile") {
				return
			}
			s.profile = name
			if existed {
				a.say("Replaced profile %s", name)
			} else {
				a.say("Saved profile %s", name)
			}
		})
		a.push(p)
		return s, cmd
	case a.is(key, "profile"):
		return s, s.pickProfile(a)
	case a.is(key, "reset"):
		s.cfg = llama.Defaults().Fix(s.env)
	case a.is(key, "copy"):
		a.copyCommand(llama.Build(s.env, s.cfg.Fix(s.env)))
	case a.is(key, "confirm"):
		if cur.Text {
			return s, s.edit(cur)
		}
		return nil, s.run(a)
	case a.is(key, "run"):
		return nil, s.run(a)
	}
	return s, nil
}

func (s *sheet) notes(a *App) []llama.Note {
	if !a.cfg.Warnings {
		return nil
	}
	return llama.Notes(s.env, s.cfg)
}

func (s *sheet) pickProfile(a *App) tea.Cmd {
	names := a.profiles.Names(s.env.Model.Ref)
	if len(names) == 0 {
		a.say("No profiles for this model yet: press %s to save these settings as one", a.keys.Label("save"))
		return nil
	}
	opts := []option{{"", "No profile", "loom's defaults"}}
	for _, name := range names {
		c, _ := a.profiles.Profile(s.env.Model.Ref, name)
		opts = append(opts, option{name, name, summary(s.env, c)})
	}
	a.push(newPicker("Profile", opts, s.profile, func(a *App, name string) tea.Cmd {
		s.load(a, name)
		return nil
	}))
	return nil
}

func (s *sheet) edit(cur llama.Setting) tea.Cmd {
	s.editing = true
	s.input.SetValue(cur.Get(s.cfg))
	s.input.CursorEnd()
	return s.input.Focus()
}

func (s *sheet) run(a *App) tea.Cmd {
	a.failed(a.profiles.Remember(s.env.Model.Ref, s.profile, s.cfg), "Couldn't remember these settings")
	cfg := s.cfg.Fix(s.env)
	if cfg.Mode == llama.Chat {
		cmd := llama.Build(s.env, cfg)
		return tea.ExecProcess(cmd.Exec(), func(err error) tea.Msg { return chatDoneMsg{err} })
	}
	host := "127.0.0.1"
	if cfg.Listen == "network" {
		host = "0.0.0.0"
	}
	port := llama.FreePort(host, cfg.Port)
	if port != cfg.Port {
		a.say("Port %d was taken, using %d", cfg.Port, port)
	}
	cfg.Port = port
	cmd := llama.Build(s.env, cfg)
	inst, err := llama.Start(a.nextID, s.env, cfg, cmd, port)
	if err != nil {
		a.complain("Could not start %s: %v", cmd.Bin, err)
		return nil
	}
	a.nextID++
	a.servers = append(a.servers, inst)
	a.tab, a.act.sc = tabActivity, scroller{}
	return nil
}

func (s *sheet) body(a *App, w, h int) []string {
	if w >= 100 {
		lw := min(max(52, w*2/5), 76)
		left := s.left(a, lw, h, false)
		right := s.right(w-lw-3, h)
		out := make([]string, h)
		for i := range out {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			out[i] = fit(l, lw) + sEdge.Render(" │ ") + r
		}
		return out
	}
	return s.left(a, w, h, true)
}

func (s *sheet) head(a *App, w int) []string {
	m := s.env.Model
	facts := []string{units.Bytes(m.Size), kindLabel(m.Kind(), m.MMProj != "")}
	if m.Info != nil {
		facts = append([]string{m.Info.Size()}, facts...)
		if m.Info.Context > 0 {
			facts = append(facts, units.Tokens(m.Info.Context)+" max context")
		}
	}
	line := strings.Join(facts, " · ")
	if m.Path == "" {
		line = "not in your library"
	}
	if m.Ref != m.Name() {
		line = m.Ref + "  ·  " + line
	}
	prof := sDim.Render("no profile")
	if s.profile != "" {
		prof = sBold.Render("profile " + s.profile)
		if saved, ok := a.profiles.Profile(m.Ref, s.profile); ok && !saved.Fix(s.env).Equal(s.cfg.Fix(s.env)) {
			prof += sWarn.Render(" (changed)")
		}
	}
	out := []string{""}
	for _, l := range wrap(line, w-3) {
		out = append(out, " "+sDim.Render(l))
	}
	return append(out, " "+prof, "")
}

func (s *sheet) left(a *App, w, h int, stacked bool) []string {
	list := s.settings()
	cur := list[s.sel]
	cmd := llama.Build(s.env, s.cfg)

	lines := s.head(a, w)
	var rows []string
	selRow := 0
	group := ""
	const labelW = 20
	valW := 22
	for i, st := range list {
		if st.Group != group {
			group = st.Group
			if len(rows) > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, " "+heading(group, w-2))
		}
		if i == s.sel {
			selRow = len(rows)
		}
		label, val := st.Label, st.Display(s.env, s.cfg)
		var line string
		switch {
		case i == s.sel && s.editing:
			line = " " + sPlain.Bold(true).Render(fit(label, labelW)) + " " + s.input.View()
		case i == s.sel:
			cell := val
			if !st.Text {
				cell = "‹ " + val + " ›"
			}
			line = sPlain.Background(cSel).Bold(true).Render(" "+fit(label, labelW)+" "+fit(cell, valW)) + sPlain.Background(cSel).Render(strings.Repeat(" ", max(0, w-labelW-valW-3)))
		default:
			line = " " + sDim.Render(fit(label, labelW)) + " " + fit(val, valW)
		}
		if stacked && i != s.sel {
			line += " " + sFlag.Render(flagsOf(cmd, st.ID))
		}
		rows = append(rows, clip(line, w))
	}

	var foot []string
	foot = append(foot, "", " "+sBold.Render(cur.Label)+sDim.Render("  "+flagsHint(cmd, cur.ID)))
	for _, l := range wrap(cur.HelpFor(s.env, s.cfg), w-3) {
		foot = append(foot, "  "+sDim.Render(l))
	}
	if s.errText != "" {
		foot = append(foot, " "+sBad.Render(s.errText))
	}
	foot = append(foot, "")
	if a.cfg.MemoryEstimate {
		foot = append(foot, s.memory(a, w)...)
	}
	for _, n := range s.notes(a) {
		st := sWarn
		if n.Danger {
			st = sBad
		}
		for i, l := range wrap(n.Text, w-5) {
			if i == 0 {
				foot = append(foot, " "+st.Render("▲ "+l))
			} else {
				foot = append(foot, "   "+st.Render(l))
			}
		}
	}

	var tail []string
	if spare := h - len(lines) - len(foot) - len(rows) - 2; stacked && spare >= 5 {
		tail = append([]string{"", " " + heading("Command", w-2)}, renderCommand(cmd, w-2, cur.ID, spare, true)...)
	}
	room := max(3, h-len(lines)-len(foot)-len(tail))
	out := append(append(append(lines, windowRows(rows, selRow, room, &s.off)...), foot...), tail...)
	return out
}

func (s *sheet) right(w, h int) []string {
	cmd := llama.Build(s.env, s.cfg)
	cur := s.settings()[s.sel]
	out := []string{"", " " + heading("How it will run", w-2), " " + sDim.Render("every flag comes from a choice on the left"), ""}
	return append(out, renderCommand(cmd, w-2, cur.ID, h-len(out), false)...)
}

func (s *sheet) memory(a *App, w int) []string {
	m := llama.Estimate(s.env, s.cfg)
	label := " " + sBold.Render("Memory") + sDim.Render("  weights ") + units.Bytes(m.Weights)
	if m.Known {
		ctx := units.Tokens(uint64(m.Context)) + " context"
		if m.Floor {
			ctx = "≥" + ctx
		}
		label += sDim.Render(" + "+ctx+" ") + units.Bytes(m.KV)
	} else {
		label += sDim.Render(" + context (unknown)")
	}
	need := m.Weights + m.KV
	if m.GPU == 0 {
		return []string{label + sDim.Render(fmt.Sprintf(" = %s, all in RAM", units.Bytes(need)))}
	}
	st := sGood
	switch hw.Assess(need, m.GPU, a.hw.RAM) {
	case hw.Tight, hw.Spills:
		st = sWarn
	case hw.TooBig:
		st = sBad
	}
	bw := max(10, min(34, w-26))
	return []string{label, " " + bar(float64(need)/float64(m.GPU), bw, st) + sDim.Render(fmt.Sprintf(" %s of %s GPU", units.Bytes(need), units.VRAM(m.GPU)))}
}

func flagsOf(c llama.Command, id string) string {
	if id == "mode" {
		return c.Bin
	}
	var parts []string
	for _, arg := range c.Args {
		if arg.Setting == id {
			parts = append(parts, strings.TrimSpace(arg.Flag+" "+shortPath(arg.Value)))
		}
	}
	if len(parts) == 0 {
		return "(llama.cpp default)"
	}
	return strings.Join(parts, " ")
}

func flagsHint(c llama.Command, id string) string {
	if id == "mode" {
		return "→ runs " + c.Bin
	}
	if f := flagsOf(c, id); f != "(llama.cpp default)" {
		return "→ " + f
	}
	return "→ no flag: llama.cpp's default"
}

func renderCommand(c llama.Command, w int, hl string, maxLines int, compact bool) []string {
	bin := " " + sBold.Render(c.Bin)
	if !compact && len(c.Args)*2+1 > maxLines {
		compact = true
	}
	out := []string{bin}
	for _, arg := range c.Args {
		val := shortPath(arg.Value)
		flag := arg.Flag
		if val != "" {
			flag += " " + val
		}
		fs, mark := sFlag, "   "
		if hl != "" && arg.Setting == hl {
			fs, mark = sFlag.Bold(true), " "+sAccent.Render("▸")+" "
		}
		if compact {
			f := fit(fs.Render(flag), 24)
			out = append(out, mark+f+" "+clip(sDim.Render(arg.Why), max(0, w-30)))
			continue
		}
		out = append(out, mark+clip(fs.Render(flag), w-4))
		for _, l := range wrap(arg.Why, w-8) {
			out = append(out, "      "+sDim.Render(l))
		}
	}
	if maxLines > 0 && len(out) > maxLines {
		out = append(out[:maxLines-1], sDim.Render(fmt.Sprintf("   … %d more lines", len(out)-maxLines+1)))
	}
	return out
}

func shortPath(v string) string {
	if strings.HasPrefix(v, "/") && len(v) > 36 {
		return "…/" + filepath.Base(v)
	}
	return v
}
