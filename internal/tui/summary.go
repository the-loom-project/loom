package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"loom/internal/gguf"
	"loom/internal/hw"
	"loom/internal/llama"
	"loom/internal/units"
)

func summary(e llama.Env, c llama.Config) string {
	var p []string
	switch {
	case c.Mode == llama.Chat:
		p = append(p, "terminal chat")
	case e.Model.Kind() == gguf.Embedding:
		p = append(p, fmt.Sprintf("embeddings :%d", c.Port))
	default:
		p = append(p, fmt.Sprintf("server :%d", c.Port))
	}
	if c.Context == 0 {
		p = append(p, "context auto")
	} else {
		p = append(p, "context "+units.Tokens(uint64(c.Context)))
	}
	switch c.Offload {
	case "cpu":
		p = append(p, "CPU only")
	case "all":
		p = append(p, "all on GPU")
	default:
		p = append(p, "GPU fit")
	}
	if c.Mode == llama.Serve {
		if c.Listen == "network" {
			p = append(p, "network")
		}
		if c.Tools {
			p = append(p, "tools on")
		}
	}
	return strings.Join(p, " · ")
}

func gpuFit(a *App, size int64) hw.Fit { return a.cfg.Limits().Assess(size, a.hw.VRAM(""), a.hw.RAM) }

func fitStyle(f hw.Fit) lipgloss.Style {
	switch f {
	case hw.Fits:
		return sGood
	case hw.Tight, hw.Spills:
		return sWarn
	case hw.TooBig:
		return sBad
	}
	return sPlain
}

func fitSeg(f hw.Fit) []seg {
	label := map[hw.Fit]string{hw.Fits: "● fits", hw.Tight: "● tight", hw.Spills: "◐ partly CPU", hw.TooBig: "○ too big"}[f]
	if label == "" {
		return nil
	}
	return styled(label, fitStyle(f))
}

func home(path string) string {
	if h, _ := os.UserHomeDir(); h != "" && strings.HasPrefix(path, h) {
		return "~" + strings.TrimPrefix(path, h)
	}
	return path
}

func kindLabel(k gguf.Kind, vision bool) string {
	s := map[gguf.Kind]string{gguf.Chat: "chat", gguf.Embedding: "embedding", gguf.NoChat: "no chat", gguf.NotLM: "can't run", gguf.Unreadable: "unreadable"}[k]
	if vision && k.Runnable() {
		s += "+vision"
	}
	return s
}
