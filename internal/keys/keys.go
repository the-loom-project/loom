package keys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Action struct {
	ID      string
	Group   string
	Help    string
	Default []string
}

var Actions = []Action{
	{"up", "Move", "Up", []string{"up", "k"}},
	{"down", "Move", "Down", []string{"down", "j"}},
	{"left", "Move", "Left / previous value", []string{"left", "h"}},
	{"right", "Move", "Right / next value", []string{"right", "l"}},
	{"page-up", "Move", "Page up", []string{"pgup", "ctrl+u"}},
	{"page-down", "Move", "Page down", []string{"pgdown", "ctrl+d"}},
	{"top", "Move", "Jump to top", []string{"home", "g"}},
	{"bottom", "Move", "Jump to bottom", []string{"end", "G"}},
	{"next-view", "Views", "Next view", []string{"tab", "right", "l"}},
	{"prev-view", "Views", "Previous view", []string{"shift+tab", "left", "h"}},
	{"view-library", "Views", "Go to Library", []string{"1"}},
	{"view-discover", "Views", "Go to Discover", []string{"2"}},
	{"view-activity", "Views", "Go to Activity", []string{"3"}},
	{"view-doctor", "Views", "Go to Doctor", []string{"4"}},
	{"view-profiles", "Views", "Go to Profiles", []string{"5"}},
	{"view-config", "Views", "Go to Config", []string{"6"}},
	{"confirm", "Everywhere", "Open / confirm", []string{"enter"}},
	{"back", "Everywhere", "Close popup / clear", []string{"esc"}},
	{"help", "Everywhere", "Show this help", []string{"?"}},
	{"quit", "Everywhere", "Quit loom", []string{"q"}},
	{"search", "Lists", "Search or filter by text", []string{"/"}},
	{"filter", "Lists", "Filter the list (Discover: author, size, tag, app)", []string{"f"}},
	{"discover", "Lists", "Find the selected model on Hugging Face (opens its Discover page)", []string{"D"}},
	{"unsupported", "Lists", "Show or hide models llama.cpp can't run", []string{"u"}},
	{"info", "Lists", "Show everything about the selected model", []string{"i"}},
	{"run", "Items", "Run the selected item (Library: with its last settings; Doctor: check again)", []string{"r"}},
	{"delete", "Items", "Delete the selected item (stops a server, cancels a download)", []string{"x", "delete"}},
	{"edit", "Items", "Edit the selected item (renames a profile, edits a text setting)", []string{"e"}},
	{"reset", "Items", "Reset to defaults (launch sheet, Config)", []string{"d"}},
	{"sort", "Lists", "Sort the table: ← → pick the column, enter flips ascending / descending", []string{"S"}},
	{"copy", "Commands", "Copy the loom command", []string{"y"}},
	{"copy-raw", "Commands", "Copy the llama.cpp command", []string{"Y"}},
	{"open", "Commands", "Open in the browser", []string{"o"}},
	{"editor", "Launch sheet", "Edit the extra flags in your editor", []string{"E"}},
	{"profile", "Launch sheet", "Load a saved profile", []string{"p"}},
	{"save", "Launch sheet", "Save the settings as a profile", []string{"s"}},
	{"details", "Activity", "Open the selected server's details", []string{"enter"}},
	{"log-up", "Activity", "Scroll the log up", []string{"pgup", "ctrl+u"}},
	{"log-down", "Activity", "Scroll the log down (to the end: follow)", []string{"pgdown", "ctrl+d"}},
	{"clear", "Activity", "Clear finished items", []string{"c"}},
}

type Map struct {
	bind     map[string][]string
	Problems []string
}

func Path() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "loom", "keys.conf")
}

func Load() *Map {
	m := &Map{bind: map[string][]string{}}
	known := map[string]bool{}
	for _, a := range Actions {
		m.bind[a.ID] = a.Default
		known[a.ID] = true
	}
	data, err := os.ReadFile(Path())
	if err != nil {
		return m
	}
	for i, line := range strings.Split(string(data), "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		id, list, ok := strings.Cut(line, "=")
		id = strings.TrimSpace(id)
		if !ok || !known[id] {
			m.Problems = append(m.Problems, fmt.Sprintf("keys.conf line %d: unknown action %q", n, id))
			continue
		}
		var ks []string
		for _, k := range strings.Split(list, ",") {
			if k = strings.TrimSpace(k); k == "space" {
				k = " "
			} else if k == "" {
				continue
			}
			ks = append(ks, k)
		}
		m.bind[id] = ks
	}
	return m
}

func (m *Map) Is(id, key string) bool {
	for _, k := range m.bind[id] {
		if k == key {
			return true
		}
	}
	return false
}

func (m *Map) Any(key string, ids ...string) bool {
	for _, id := range ids {
		if m.Is(id, key) {
			return true
		}
	}
	return false
}

func (m *Map) Label(id string) string {
	ks := m.bind[id]
	if len(ks) == 0 {
		return ""
	}
	return Pretty(ks[0])
}

func (m *Map) Labels(id string) string {
	var out []string
	for _, k := range m.bind[id] {
		out = append(out, Pretty(k))
	}
	if len(out) == 0 {
		return "unbound"
	}
	return strings.Join(out, "  ")
}

var pretty = map[string]string{
	"enter": "↵", "up": "↑", "down": "↓", "left": "←", "right": "→", " ": "space", "pgup": "PgUp", "pgdown": "PgDn",
	"shift+tab": "⇧tab", "backspace": "⌫", "delete": "del",
}

func Pretty(k string) string {
	if p, ok := pretty[k]; ok {
		return p
	}
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
		return "^" + strings.ToUpper(rest)
	}
	return k
}

func (m *Map) Dump() string {
	var b strings.Builder
	b.WriteString("# loom key bindings: action = key, key, …   (use \"space\" for the space bar)\n")
	fmt.Fprintf(&b, "# Save as %s\n", Path())
	group := ""
	for _, a := range Actions {
		if a.Group != group {
			group = a.Group
			fmt.Fprintf(&b, "\n# %s\n", group)
		}
		ks := make([]string, len(m.bind[a.ID]))
		for i, k := range m.bind[a.ID] {
			ks[i] = k
			if k == " " {
				ks[i] = "space"
			}
		}
		fmt.Fprintf(&b, "%-14s = %-18s # %s\n", a.ID, strings.Join(ks, ", "), a.Help)
	}
	return b.String()
}
