package tui

import "strings"

const tagRows = 4

func tagBlock(a *App, tags []string, w int) []string {
	out := []string{" " + sDim.Render("tags") + " " + sEdge.Render(strings.Repeat("─", max(0, w-7)))}
	if len(tags) == 0 {
		out = append(out, " "+sDim.Render("none"))
	}
	room := w - 2
	rowWidth := func(row []string) int { return width(strings.Join(row, "  ")) }
	var rows [][]string
	for _, tag := range tags {
		if n := len(rows); n > 0 && rowWidth(append(rows[n-1], tag)) <= room {
			rows[n-1] = append(rows[n-1], tag)
		} else {
			rows = append(rows, []string{tag})
		}
	}
	more := len(rows) > tagRows-1
	if more {
		rows = rows[:tagRows-1]
		last := &rows[len(rows)-1]
		for len(*last) > 1 && rowWidth(*last)+3 > room {
			*last = (*last)[:len(*last)-1]
		}
	}
	for i, row := range rows {
		styled := make([]string, len(row))
		for j, tag := range row {
			st := sDim
			if a.cfg.Highlighted(tag) {
				st = sAccent.Bold(true)
			}
			styled[j] = st.Render(tag)
		}
		line := " " + strings.Join(styled, "  ")
		if more && i == len(rows)-1 {
			line += sDim.Render("  …")
		}
		out = append(out, clip(line, w))
	}
	for len(out) < tagRows {
		out = append(out, "")
	}
	return out
}
