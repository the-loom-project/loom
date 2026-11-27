package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type sortCol struct {
	id   string
	desc bool // the direction of the first press
}

// sorter is a table's sort order. While picking, the arrow keys choose the
// column, enter flips the direction and any other key ends it.
type sorter struct {
	col     int
	desc    bool
	on      bool
	picking bool
}

func (s *sorter) start(cols []sortCol) {
	s.picking = true
	if !s.on {
		s.on, s.col, s.desc = true, 0, cols[0].desc
	}
}

// key handles a key press while picking, and reports whether it did.
func (s *sorter) key(a *App, msg tea.KeyMsg, cols []sortCol) bool {
	if !s.picking {
		return false
	}
	move := func(d int) {
		s.col = (s.col + d + len(cols)) % len(cols)
		s.desc = cols[s.col].desc
	}
	switch {
	case a.is(msg, "left"):
		move(-1)
	case a.is(msg, "right"), a.is(msg, "sort"):
		if a.is(msg, "sort") {
			s.picking = false
		} else {
			move(1)
		}
	case a.is(msg, "confirm"):
		s.desc = !s.desc
	case a.is(msg, "reset"):
		*s = sorter{}
	case a.is(msg, "back"), a.is(msg, "quit"):
		s.picking = false
	}
	return true
}

func (s sorter) hints(a *App) string {
	return a.hints("left", "column", "confirm", "ascending / descending", "reset", "clear", "back", "done")
}

func (s sorter) active(cols []sortCol) (id string, desc, on bool) {
	if !s.on {
		return "", false, false
	}
	return cols[s.col].id, s.desc, true
}

func sortedBy[T any](list []T, s sorter, cols []sortCol, cmps map[string]func(a, b T) int) []T {
	id, desc, on := s.active(cols)
	if !on {
		return list
	}
	out := slices.Clone(list)
	slices.SortStableFunc(out, func(a, b T) int {
		if desc {
			return cmps[id](b, a)
		}
		return cmps[id](a, b)
	})
	return out
}

func cmpText(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }

func cmpNum[N int | int64 | uint64](a, b N) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
