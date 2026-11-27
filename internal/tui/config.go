package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"loom/internal/config"
)

type opt struct {
	group, label, help string
	text               bool
	empty              string
	choices            []option
	get                func(*config.Config) string
	set                func(*config.Config, string) error
}

var shownHidden = []option{{"true", "Shown", ""}, {"false", "Hidden", ""}}

func toggle(group, label, help string, get func(*config.Config) *bool) opt {
	return opt{group: group, label: label, help: help, choices: shownHidden,
		get: func(c *config.Config) string { return strconv.FormatBool(*get(c)) },
		set: func(c *config.Config, v string) error { *get(c) = v == "true"; return nil }}
}

func percent(group, label, help string, lo, hi int, get func(*config.Config) *int) opt {
	return opt{group: group, label: label, help: help, text: true,
		get: func(c *config.Config) string { return strconv.Itoa(*get(c)) + "%" },
		set: func(c *config.Config, v string) error {
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), "%"))
			if err != nil || n < lo || n > hi {
				return fmt.Errorf("a number from %d to %d", lo, hi)
			}
			*get(c) = n
			return nil
		}}
}

func columnPref(group, view, id, label, help string) opt {
	return opt{group: group, label: label, help: help, choices: shownHidden,
		get: func(c *config.Config) string { return strconv.FormatBool(!c.Hidden(view, id)) },
		set: func(c *config.Config, v string) error { c.SetHidden(view, id, v != "true"); return nil }}
}

func options() []opt {
	var langs []option
	for _, l := range config.Languages {
		langs = append(langs, option{l.Code, l.Name, ""})
	}
	return []opt{
		{group: "General", label: "Language", help: "The language of the interface. English is the only one so far.", choices: langs,
			get: func(c *config.Config) string { return c.Language }, set: func(c *config.Config, v string) error { c.Language = v; return nil }},
		{group: "General", label: "Editor", text: true,
			help: "The command that opens your extra llama.cpp flags (E in the launch sheet). It takes over this terminal while you edit, e.g. nvim. Empty uses $VISUAL, then $EDITOR, then vi.",
			get:  func(c *config.Config) string { return c.Editor }, set: func(c *config.Config, v string) error { c.Editor = strings.TrimSpace(v); return nil }},

		toggle("Show", "Key hints", "The line of keys at the bottom of the screen. ? still lists every key.", func(c *config.Config) *bool { return &c.KeyHints }),
		toggle("Show", "Memory estimate", "The memory bar in the launch sheet: how much of your GPU the model and its context would use.", func(c *config.Config) *bool { return &c.MemoryEstimate }),
		toggle("Show", "Warnings", "The ▲ notes in the launch sheet, such as a model that won't fit or a server open to the network.", func(c *config.Config) *bool { return &c.Warnings }),

		{group: "Tags", label: "Highlighted tags", text: true, empty: "none",
			help: "Tags that are always highlighted in the tag list under the Discover table, separated by spaces or commas. End one with * to match a prefix, e.g. license:* or base_model:*.",
			get:  func(c *config.Config) string { return strings.Join(c.HighlightTags, ", ") },
			set: func(c *config.Config, v string) error {
				c.HighlightTags = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
				return nil
			}},

		percent("Colors", "Green up to", "Sizes in Discover are green while at most this share of your GPU memory. Lower it to leave more room for the context window.", 1, 100, func(c *config.Config) *int { return &c.GreenPercent }),
		percent("Colors", "Red beyond", "Sizes in Discover are red when bigger than your GPU memory plus this share of your system RAM, so the model can't be spread across both. Yellow is everything in between.", 0, 200, func(c *config.Config) *int { return &c.RedRAMPercent }),

		columnPref("Library columns", "library", "author", "Author", "The user or organization that published the model."),
		columnPref("Library columns", "library", "updated", "Updated", "When the model last changed on Hugging Face, or ↑ update when a newer version is available. Hidden on narrow terminals anyway."),
		columnPref("Library columns", "library", "params", "Parameters", "How many billions of parameters the model has."),
		columnPref("Library columns", "library", "quant", "Quantization", "How compressed the weights are (Q4_K_M, Q8_0, …). Hidden on narrow terminals anyway."),
		columnPref("Library columns", "library", "size", "Size", "The size of the model on disk."),
		columnPref("Library columns", "library", "context", "Context", "The longest context window the model supports. Hidden on narrow terminals anyway."),
		columnPref("Library columns", "library", "type", "Type", "Chat, embedding, vision and so on."),
		columnPref("Library columns", "library", "gpu", "GPU fit", "Whether the model fits in your GPU's memory."),

		columnPref("Discover columns", "discover", "author", "Author", "The user or organization that published the model."),
		columnPref("Discover columns", "discover", "params", "Parameters", "How many billions of parameters the model has."),
		columnPref("Discover columns", "discover", "size", "Size", "The smallest and largest download of the model across its quantizations, colored by whether it fits your GPU. Looked up for the rows on screen."),
		columnPref("Discover columns", "discover", "downloads", "Downloads", "How often it was downloaded from Hugging Face."),
		columnPref("Discover columns", "discover", "likes", "Likes", "Its likes on Hugging Face."),
		columnPref("Discover columns", "discover", "updated", "Updated", "When it was last changed. Hidden on narrow terminals anyway."),
	}
}

type configView struct {
	list    []opt
	sc      scroller
	editing bool
	input   textinput.Model
	off     int
	errText string
}

func newConfigView() *configView {
	ti := textinput.New()
	ti.Prompt = ""
	return &configView{list: options(), input: ti}
}

func (v *configView) typing() bool { return v.editing }

func (v *configView) hints(a *App) string {
	if v.editing {
		return a.hints("confirm", "accept", "back", "cancel")
	}
	return a.hints("confirm", "change", "reset", "reset all", "help", "all keys")
}

func (v *configView) display(a *App, p opt) string {
	cur := p.get(a.cfg)
	if p.text {
		if cur == "" && p.empty != "" {
			return p.empty
		}
		if cur == "" {
			return "default"
		}
		return cur
	}
	for _, o := range p.choices {
		if o.value == cur {
			return o.label
		}
	}
	return cur
}

func (v *configView) cycle(a *App, p opt) {
	if p.text || len(p.choices) < 2 {
		return
	}
	cur := 0
	for i, o := range p.choices {
		if o.value == p.get(a.cfg) {
			cur = i
		}
	}
	p.set(a.cfg, p.choices[(cur+1)%len(p.choices)].value)
	a.cfg.Save()
}

func (v *configView) update(a *App, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	p := v.list[min(v.sc.cur, len(v.list)-1)]
	if v.editing {
		switch {
		case a.is(key, "confirm"):
			if err := p.set(a.cfg, v.input.Value()); err != nil {
				v.errText = "Needs " + err.Error()
				return nil
			}
			a.cfg.Save()
			v.editing, v.errText = false, ""
		case a.is(key, "back"):
			v.editing, v.errText = false, ""
		default:
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			return cmd
		}
		return nil
	}
	switch {
	case a.is(key, "up"):
		v.sc.move(-1, len(v.list))
	case a.is(key, "down"):
		v.sc.move(1, len(v.list))
	case a.is(key, "page-up"):
		v.sc.move(-10, len(v.list))
	case a.is(key, "page-down"):
		v.sc.move(10, len(v.list))
	case a.is(key, "reset"):
		a.push(&confirm{heading: "Reset the config?", text: "Language, editor and everything shown or hidden go back to their defaults.", yes: func(a *App) tea.Cmd {
			a.cfg.Reset()
			a.say("Config reset to defaults")
			return nil
		}})
	case a.is(key, "top"):
		v.sc.cur = 0
	case a.is(key, "bottom"):
		v.sc.cur = len(v.list) - 1
	case a.is(key, "confirm"), a.is(key, "edit") && p.text:
		if p.text {
			v.editing = true
			v.input.SetValue(p.get(a.cfg))
			v.input.CursorEnd()
			return v.input.Focus()
		}
		v.cycle(a, p)
	}
	return nil
}

func (v *configView) view(a *App, w, h int) []string {
	const labelW, valW = 22, 16
	var rows []string
	selRow, group := 0, ""
	for i, p := range v.list {
		if p.group != group {
			group = p.group
			if len(rows) > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, " "+heading(group, w-2))
		}
		if i == v.sc.cur {
			selRow = len(rows)
		}
		val := v.display(a, p)
		switch {
		case i == v.sc.cur && v.editing:
			rows = append(rows, " "+sBold.Render(fit(p.label, labelW))+" "+v.input.View())
		case i == v.sc.cur:
			rows = append(rows, sPlain.Background(cSel).Bold(true).Render(" "+fit(p.label, labelW)+" "+fit(val, valW))+sPlain.Background(cSel).Render(strings.Repeat(" ", max(0, w-labelW-valW-3))))
		default:
			rows = append(rows, " "+sDim.Render(fit(p.label, labelW))+" "+fit(val, valW))
		}
	}
	cur := v.list[v.sc.cur]
	foot := []string{"", sEdge.Render(strings.Repeat("─", w)), " " + sBold.Render(cur.label)}
	for _, l := range wrap(cur.help, w-4) {
		foot = append(foot, "  "+sDim.Render(l))
	}
	if v.errText != "" {
		foot = append(foot, "", " "+sBad.Render(v.errText))
	}
	foot = append(foot, "  "+sDim.Render("Saved to "+home(config.Path())+" as you change it."))
	room := max(3, h-1-len(foot))
	return append(append([]string{""}, windowRows(rows, selRow, room, &v.off)...), foot...)
}
