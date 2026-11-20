package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"loom/internal/llama"
)

func color(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

var (
	cAccent = color("#5B4BC4", "#9D8CFF")
	cGood   = color("#1F7A3D", "#5FD38D")
	cWarn   = color("#9A5B00", "#F2B84B")
	cBad    = color("#B3261E", "#FF7B72")
	cDim    = color("#6B7280", "#7D8590")
	cFlag   = color("#0B6E8A", "#6CC4DE")
	cSel    = color("#E4E6F5", "#2A2D45")
	cEdge   = color("#B8BCC8", "#3D4152")

	sDim    = lipgloss.NewStyle().Foreground(cDim)
	sAccent = lipgloss.NewStyle().Foreground(cAccent)
	sGood   = lipgloss.NewStyle().Foreground(cGood)
	sWarn   = lipgloss.NewStyle().Foreground(cWarn)
	sBad    = lipgloss.NewStyle().Foreground(cBad)
	sFlag   = lipgloss.NewStyle().Foreground(cFlag)
	sBold   = lipgloss.NewStyle().Bold(true)
	sPlain  = lipgloss.NewStyle()
	sEdge   = lipgloss.NewStyle().Foreground(cEdge)
)

func width(s string) int { return ansi.StringWidth(s) }

func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func fit(s string, w int) string {
	s = clip(s, w)
	return s + strings.Repeat(" ", max(0, w-width(s)))
}

func right(s string, w int) string {
	s = clip(s, w)
	return strings.Repeat(" ", max(0, w-width(s))) + s
}

func between(left, rt string, w int) string {
	gap := w - width(left) - width(rt)
	if gap < 1 {
		return clip(left, max(0, w-width(rt)-1)) + " " + rt
	}
	return left + strings.Repeat(" ", gap) + rt
}

func wrap(text string, w int) []string {
	if w < 8 {
		w = 8
	}
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case width(line)+1+width(word) <= w:
				line += " " + word
			default:
				lines = append(lines, line)
				line = word
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func bar(frac float64, w int, st lipgloss.Style) string {
	frac = min(1, max(0, frac))
	n := int(frac*float64(w) + 0.5)
	return st.Render(strings.Repeat("█", n)) + sDim.Render(strings.Repeat("░", w-n))
}

func heading(title string, w int) string {
	return sBold.Render(title) + " " + sEdge.Render(strings.Repeat("─", max(0, w-width(title)-1)))
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spin(tick int) string { return sAccent.Render(spinFrames[tick%len(spinFrames)]) }

type scroller struct{ cur, off int }

func (s *scroller) move(delta, n int) { s.cur = min(max(0, s.cur+delta), max(0, n-1)) }

func (s *scroller) window(n, h int) (int, int) {
	s.cur = min(s.cur, max(0, n-1))
	if h >= n {
		s.off = 0
		return 0, n
	}
	s.off = min(max(s.off, s.cur-h+1), s.cur)
	s.off = min(s.off, n-h)
	return s.off, s.off + h
}

type seg struct {
	text  string
	style lipgloss.Style
}

func t(text string, st lipgloss.Style) seg { return seg{text, st} }

type column struct {
	title string
	w     int
	right bool
	min   int
}

type table struct {
	cols   []column
	widths []int
	total  int
}

func newTable(total int, cols ...column) table {
	tb := table{cols: cols, widths: make([]int, len(cols)), total: total}
	used, shown := 1, 0
	flex := -1
	for i, c := range cols {
		if total < c.min {
			continue
		}
		shown++
		tb.widths[i] = c.w
		if c.w == 0 {
			flex = i
		}
		used += c.w
	}
	used += 2 * (shown - 1)
	if flex >= 0 {
		tb.widths[flex] = max(10, total-used-1)
	}
	return tb
}

func (tb table) header() string {
	var parts []string
	for i, c := range tb.cols {
		if tb.widths[i] == 0 {
			continue
		}
		if c.right {
			parts = append(parts, right(c.title, tb.widths[i]))
		} else {
			parts = append(parts, fit(c.title, tb.widths[i]))
		}
	}
	return sDim.Render(" " + strings.Join(parts, "  "))
}

func (tb table) row(selected bool, cells ...[]seg) string {
	var b strings.Builder
	paint := func(text string, st lipgloss.Style) {
		if text == "" {
			return
		}
		if selected {
			st = st.Background(cSel)
		}
		b.WriteString(st.Render(text))
	}
	paint(" ", sPlain)
	first := true
	for i, c := range tb.cols {
		w := tb.widths[i]
		if w == 0 {
			continue
		}
		if !first {
			paint("  ", sPlain)
		}
		var segs []seg
		if i < len(cells) {
			segs = cells[i]
		}
		for _, s := range layout(segs, w, c.right) {
			if selected && first {
				s.style = s.style.Bold(true)
			}
			paint(s.text, s.style)
		}
		first = false
	}
	paint(strings.Repeat(" ", max(0, tb.total-width(strings.TrimRight(ansi.Strip(b.String()), "")))), sPlain)
	return b.String()
}

func layout(segs []seg, w int, rightAlign bool) []seg {
	var out []seg
	used := 0
	for _, s := range segs {
		if used >= w {
			break
		}
		text := clip(s.text, w-used)
		used += width(text)
		out = append(out, seg{text, s.style})
	}
	pad := seg{strings.Repeat(" ", max(0, w-used)), sPlain}
	if rightAlign {
		return append([]seg{pad}, out...)
	}
	return append(out, pad)
}

func plain(text string) []seg                     { return []seg{{text, sPlain}} }
func dim(text string) []seg                       { return []seg{{text, sDim}} }
func styled(text string, st lipgloss.Style) []seg { return []seg{{text, st}} }

func Explain(c llama.Command, w int) string {
	return strings.Join(renderCommand(c, w, "", 1000, false), "\n")
}
