package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"loom/internal/llama"
)

var (
	hHead = lipgloss.NewStyle().Bold(true)
	hCmd  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#5B4BC4", Dark: "#9D8CFF"}).Bold(true)
	hFlag = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#0B6E8A", Dark: "#6CC4DE"})
	hArg  = hFlag.Italic(true)
	hVal  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9A5B00", Dark: "#F2B84B"})
	hDim  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#7D8590"})
)

type helpRow struct{ usage, what string }

func helpTable(rows []helpRow) string {
	w := 0
	for _, r := range rows {
		w = max(w, len([]rune(r.usage)))
	}
	var b strings.Builder
	for _, r := range rows {
		pad := strings.Repeat(" ", w-len([]rune(r.usage)))
		fmt.Fprintf(&b, "  %s%s  %s\n", colorUsage(r.usage), pad, hDim.Render(r.what))
	}
	return b.String()
}

func colorUsage(u string) string {
	var out []string
	for i, w := range strings.Fields(u) {
		out = append(out, colorToken(w, i))
	}
	return strings.Join(out, " ")
}

func colorToken(w string, pos int) string {
	switch {
	case strings.HasPrefix(w, "<") || strings.HasPrefix(w, "[") && !strings.HasPrefix(w, "[-"):
		return hArg.Render(w)
	case strings.HasPrefix(w, "[-"):
		inner := strings.TrimRight(strings.TrimLeft(w, "["), "]")
		return hDim.Render("[") + hFlag.Render(inner) + hDim.Render(strings.Repeat("]", len(w)-len(strings.TrimRight(w, "]"))))
	case strings.HasPrefix(w, "-"):
		return hFlag.Render(w)
	}
	if key, vals, ok := strings.Cut(w, "="); ok && key != "" && !strings.ContainsAny(key, "<>") {
		return hCmd.Render(key) + hDim.Render("=") + colorValues(vals)
	}
	if pos <= 1 && !strings.Contains(w, "/") {
		return hCmd.Render(w)
	}
	return w
}

func colorValues(vals string) string {
	parts := strings.Split(vals, "|")
	for i, p := range parts {
		if strings.HasPrefix(p, "<") {
			parts[i] = hArg.Render(p)
		} else {
			parts[i] = hVal.Render(p)
		}
	}
	return strings.Join(parts, hDim.Render("|"))
}

func section(title string, rows []helpRow) string {
	return "\n" + hHead.Render(title) + "\n" + helpTable(rows)
}

func examples(lines ...string) string {
	var b strings.Builder
	b.WriteString("\n" + hHead.Render("Examples") + "\n")
	for _, l := range lines {
		b.WriteString("  " + colorUsage(l) + "\n")
	}
	return b.String()
}

func head(usage, what string) string {
	return hHead.Render("Usage") + "  " + colorUsage(usage) + "\n" + hDim.Render(what) + "\n"
}

func helpText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", hCmd.Render("loom"), hDim.Render("- find, download and run local models with llama.cpp"))
	b.WriteString(section("Commands", []helpRow{
		{"loom", "open the interface: browse, download and run models"},
		{"loom run <model> [options]", "start a model; every flag is explained as it starts"},
		{"loom pull <owner/repo[:QUANT]>", "download from Hugging Face"},
		{"loom ls", "list downloaded models"},
		{"loom rm [-y] <model>", "delete a model"},
		{"loom keys", "print the key bindings, to customize them"},
		{"loom completion fish", "print the fish completions"},
	}))
	b.WriteString("\n" + hDim.Render("loom help <command> (or loom <command> --help) shows a command's options.") + "\n")
	b.WriteString(hDim.Render("In the interface, press ? for the keys.") + "\n")
	return b.String()
}

func helpFor(cmd string) (string, bool) {
	var b strings.Builder
	switch cmd {
	case "run":
		b.WriteString(head("loom run <model> [options] [-- <flags>]", "Start a model. loom prints the command it builds, explains every flag, then runs it."))
		b.WriteString(hDim.Render("<model> is a name from `loom ls` (a unique part is enough), or owner/repo:QUANT, which is downloaded first.") + "\n")
		b.WriteString(section("Options", []helpRow{
			{"--chat", "chat in this terminal instead of starting a server"},
			{"--ctx <tokens>", "context window (default: auto)"},
			{"--port <n>", "server port (default: the last one used, else 8080)"},
			{"--lan", "let other machines on your network connect"},
			{"--tools", "let the model read/write files and run commands (dangerous)"},
			{"--cpu", "don't use the GPU"},
			{"--set <setting>=<value>", "any setting from the launch sheet; repeatable"},
			{"-- <flags>", "everything after -- goes to llama.cpp unchanged"},
		}))
		b.WriteString(section("Settings for --set", settingRows()))
		b.WriteString(examples(
			"loom run qwen3-8b",
			"loom run qwen3-8b --ctx 16384 --lan",
			"loom run qwen3-coder-30b --set experts=cpu --set kv=q8_0",
			"loom run phi4 --chat -- --temp 0.3",
		))
		b.WriteString("\n" + hDim.Render("Your last choices for a model are remembered; options here override them for this run.") + "\n")
	case "pull":
		b.WriteString(head("loom pull <owner/repo[:QUANT]>", "Download a model from Hugging Face into loom's model folder."))
		b.WriteString(hDim.Render("Without :QUANT it lists the quantizations, what fits your GPU, and the one loom recommends.") + "\n")
		b.WriteString(examples("loom pull unsloth/Qwen3-8B-GGUF", "loom pull unsloth/Qwen3-8B-GGUF:Q4_K_M"))
		b.WriteString("\n" + hDim.Render("Gated models need a token: set HF_TOKEN, or log in with `hf auth login`. Interrupted downloads resume.") + "\n")
	case "ls":
		b.WriteString(head("loom ls", "List downloaded models with size, type, maximum context and whether they fit your GPU."))
	case "rm":
		b.WriteString(head("loom rm [-y] <model>", "Delete a downloaded model (and any files only it uses)."))
		b.WriteString(section("Options", []helpRow{{"-y, --yes", "don't ask first"}}))
	case "keys":
		b.WriteString(head("loom keys", "Print every key binding in keys.conf format."))
		b.WriteString(examples("loom keys > ~/.config/loom/keys.conf"))
		b.WriteString("\n" + hDim.Render("Edit a line to rebind (filter = f, ctrl+o), then restart loom. Missing lines keep their defaults.") + "\n")
	case "completion":
		b.WriteString(head("loom completion fish", "Print the fish completions. Model names complete from what you have downloaded."))
		b.WriteString(examples("loom completion fish > ~/.config/fish/completions/loom.fish"))
	default:
		return "", false
	}
	return b.String(), true
}

func settingRows() []helpRow {
	var rows []helpRow
	for _, id := range settingIDs {
		st, ok := llama.SettingByID(id)
		if !ok {
			continue
		}
		vals := "<text>"
		if !st.Text {
			var v []string
			for _, ch := range st.Choices(llama.Env{}) {
				v = append(v, ch.Value)
			}
			vals = strings.Join(v, "|")
			if len(v) > 8 {
				vals = "<number>"
			}
		}
		if over, ok := map[string]string{"device": "<id>", "port": "<port>", "context": "auto|<tokens>"}[id]; ok {
			vals = over
		}
		rows = append(rows, helpRow{id + "=" + vals, st.Label})
	}
	return rows
}

var settingIDs = []string{"mode", "context", "offload", "device", "experts", "vision", "listen", "port", "tools", "parallel", "kv", "flash", "threads", "extra"}
