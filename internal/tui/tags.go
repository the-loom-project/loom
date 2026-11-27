package tui

import "strings"

const tagRows = 4

func tagBlock(a *App, tags []string, w int) []string {
	out := []string{" " + sDim.Render("tags") + " " + sEdge.Render(strings.Repeat("─", max(0, w-7)))}
	if len(tags) == 0 {
		out = append(out, " "+sDim.Render("none"))
	}
	var rows []string
	row, rowW := "", 0
	for _, tag := range tags {
		if rowW > 0 && rowW+2+width(tag) > w-2 {
			rows = append(rows, row)
			row, rowW = "", 0
		}
		st := sDim
		if a.cfg.Highlighted(tag) {
			st = sAccent.Bold(true)
		}
		if rowW > 0 {
			row += "  "
			rowW += 2
		}
		row += st.Render(tag)
		rowW += width(tag)
	}
	if row != "" {
		rows = append(rows, row)
	}
	if len(rows) > tagRows-1 {
		rows = rows[:tagRows-1]
		rows[tagRows-2] += sDim.Render("  …")
	}
	for _, r := range rows {
		out = append(out, " "+r)
	}
	for len(out) < tagRows {
		out = append(out, "")
	}
	return out
}
