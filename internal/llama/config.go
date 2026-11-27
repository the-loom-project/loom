package llama

import (
	"strconv"
	"strings"

	"loom/internal/gguf"
	"loom/internal/hw"
	"loom/internal/store"
)

type Mode string

const (
	Serve Mode = "serve"
	Chat  Mode = "chat"
)

type Config struct {
	Mode     Mode   `json:"mode"`
	Context  int    `json:"context"`
	Offload  string `json:"offload"`
	Device   string `json:"device"`
	Experts  string `json:"experts"`
	KVCache  string `json:"kv_cache"`
	Flash    string `json:"flash"`
	Threads  int    `json:"threads"`
	Parallel int    `json:"parallel"`
	Port     int    `json:"port"`
	Listen   string `json:"listen"`
	Tools    bool   `json:"tools"`
	Vision   bool   `json:"vision"`
	Extra    string `json:"extra"`
}

type Env struct {
	Model store.Model
	HW    hw.Hardware
}

func Defaults() Config {
	return Config{
		Mode: Serve, Offload: "auto", Experts: "gpu", KVCache: "f16", Flash: "auto",
		Port: 8080, Listen: "local", Vision: true,
	}
}

func (c Config) Fix(e Env) Config {
	d := Defaults()
	pick := func(v, def string, ok ...string) string {
		for _, o := range ok {
			if v == o {
				return v
			}
		}
		return def
	}
	if c.Mode != Serve && c.Mode != Chat {
		c.Mode = d.Mode
	}
	c.Offload = pick(c.Offload, d.Offload, "auto", "all", "cpu")
	c.Experts = pick(c.Experts, d.Experts, "gpu", "cpu")
	c.KVCache = pick(c.KVCache, d.KVCache, "f16", "q8_0", "q4_0")
	c.Flash = pick(c.Flash, d.Flash, "auto", "on", "off")
	c.Listen = pick(c.Listen, d.Listen, "local", "network")
	if c.Port < 1024 || c.Port > 65535 {
		c.Port = d.Port
	}
	if c.Context < 0 {
		c.Context = 0
	}
	known := c.Device == ""
	for _, dev := range e.HW.Devices {
		known = known || dev.ID == c.Device
	}
	if !known {
		c.Device = ""
	}
	if e.Model.Kind() == gguf.Embedding {
		c.Mode = Serve
	}
	if e.Model.MMProj == "" {
		c.Vision = false
	}
	return c
}

func itoa(n int) string { return strconv.Itoa(n) }

// LoomArgs is the `loom run` command that reproduces c, written as changes
// from base: the defaults, or the named profile when profile is set.
func (c Config) LoomArgs(e Env, base Config, profile string) []string {
	d := base
	short := map[string]string{"mode": "--chat", "tools": "--tools", "listen": "--lan", "offload": "--cpu", "context": "--ctx", "port": "--port"}
	out := []string{"loom", "run", shellWord(e.Model.Ref)}
	if profile != "" {
		out = append(out, "--profile", profile)
	}
	for _, st := range allSettings {
		if st.ID == "extra" || st.Get(c) == st.Get(d) || !st.IsVisible(e, c) {
			continue
		}
		v := st.Get(c)
		switch {
		case st.ID == "mode" && v == string(Chat), st.ID == "tools" && v == "true", st.ID == "listen" && v == "network", st.ID == "offload" && v == "cpu":
			out = append(out, short[st.ID])
		case (st.ID == "context" || st.ID == "port") && v != "0":
			out = append(out, short[st.ID], v)
		default:
			out = append(out, "--set", st.ID+"="+v)
		}
	}
	if extra := strings.Fields(c.Extra); len(extra) > 0 {
		out = append(out, "--")
		out = append(out, extra...)
	}
	return out
}

func shellWord(s string) string { return shellQuote(s) }

func Shell(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = shellQuote(w)
	}
	return strings.Join(q, " ")
}
