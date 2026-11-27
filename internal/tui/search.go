package tui

import "github.com/charmbracelet/bubbles/textinput"

// searchLine is the search box shown above a list, only while typing or while
// a search is applied.
func searchLine(in *textinput.Model, typing bool, w int) (string, bool) {
	if !typing && in.Value() == "" {
		return "", false
	}
	in.Width = max(10, w-14)
	label := sDim.Render("search")
	if typing {
		label = sAccent.Render("search")
	}
	return " " + label + sDim.Render("  ") + in.View(), true
}

// filterLine shows an active filter above a list.
func filterLine(what string) string { return " " + sAccent.Render("filter") + "  " + what }
