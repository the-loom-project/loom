package config

import (
	"loom/internal/jsonfile"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Language struct{ Code, Name string }

var Languages = []Language{{"en", "English"}}

type Config struct {
	Language       string   `json:"language"`
	Editor         string   `json:"editor"`
	KeyHints       bool     `json:"key_hints"`
	MemoryEstimate bool     `json:"memory_estimate"`
	Warnings       bool     `json:"warnings"`
	GreenPercent   int      `json:"green_percent"`
	RedRAMPercent  int      `json:"red_ram_percent"`
	HighlightTags  []string `json:"highlight_tags"`
	HideLibrary    []string `json:"hide_library_columns"`
	HideDiscover   []string `json:"hide_discover_columns"`
	Timeout        int      `json:"timeout_seconds"`

	path string
}

func Defaults() *Config {
	return &Config{Language: "en", KeyHints: true, MemoryEstimate: true, Warnings: true, GreenPercent: 70, RedRAMPercent: 90, Timeout: 30}
}

// Reset puts every option back to its default and saves.
func (c *Config) Reset() error {
	path := c.path
	*c = *Defaults()
	c.path = path
	return c.Save()
}

func Path() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "loom", "config.json")
}

func Load() (*Config, error) {
	c := Defaults()
	c.path = Path()
	err := jsonfile.Read(c.path, c)
	if err != nil {
		*c = *Defaults()
		c.path = Path()
	}
	if c.GreenPercent < 1 || c.GreenPercent > 100 {
		c.GreenPercent = Defaults().GreenPercent
	}
	if c.RedRAMPercent < 0 || c.RedRAMPercent > 200 {
		c.RedRAMPercent = Defaults().RedRAMPercent
	}
	if c.Timeout < 5 || c.Timeout > 600 {
		c.Timeout = Defaults().Timeout
	}
	if !slices.ContainsFunc(Languages, func(l Language) bool { return l.Code == c.Language }) {
		c.Language = Languages[0].Code
	}
	return c, err
}

func (c *Config) Save() error { return jsonfile.Write(c.path, c) }

// EditorCmd returns the command that opens a file for editing: the configured
// editor, else $VISUAL, else $EDITOR, else vi.
func (c *Config) EditorCmd() []string {
	for _, e := range []string{c.Editor, os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi"} {
		if f := strings.Fields(e); len(f) > 0 {
			return f
		}
	}
	return nil
}

func (c *Config) NetTimeout() time.Duration { return time.Duration(c.Timeout) * time.Second }

func (c *Config) hidden(view string) *[]string {
	if view == "library" {
		return &c.HideLibrary
	}
	return &c.HideDiscover
}

func (c *Config) Highlighted(tag string) bool {
	tag = strings.ToLower(tag)
	for _, p := range c.HighlightTags {
		p = strings.ToLower(p)
		if prefix, ok := strings.CutSuffix(p, "*"); ok && strings.HasPrefix(tag, prefix) || p == tag {
			return true
		}
	}
	return false
}

func (c *Config) Hidden(view, column string) bool { return slices.Contains(*c.hidden(view), column) }

func (c *Config) SetHidden(view, column string, hide bool) {
	list := c.hidden(view)
	*list = slices.DeleteFunc(*list, func(s string) bool { return s == column })
	if hide {
		*list = append(*list, column)
	}
}
