package units

import (
	"fmt"
	"time"
)

func Bytes(n int64) string {
	switch {
	case n >= 99.95e9:
		return fmt.Sprintf("%.0f GB", float64(n)/1e9)
	case n >= 999.5e6:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 999.5e3:
		return fmt.Sprintf("%.0f MB", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.0f kB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}

func GiB(n int64) string {
	g := float64(n) / (1 << 30)
	if g >= 10 {
		return fmt.Sprintf("%.0f GiB", g)
	}
	return fmt.Sprintf("%.1f GiB", g)
}

func Count(n int) string {
	switch {
	case n >= 999_500:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 9_950:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func Tokens(n uint64) string {
	switch {
	case n == 0:
		return "?"
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%dM", n>>20)
	case n >= 1024:
		return fmt.Sprintf("%dk", (n+512)/1024)
	}
	return fmt.Sprint(n)
}

func Ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case t.IsZero():
		return ""
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 700*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
}

func Duration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
