package llama

import (
	"fmt"
	"os"
	"strings"

	"loom/internal/gguf"
	"loom/internal/hw"
)

type Arg struct {
	Flag    string
	Value   string
	Why     string
	Setting string
}

func (a Arg) Words() []string {
	if a.Value == "" {
		return []string{a.Flag}
	}
	return []string{a.Flag, a.Value}
}

type Command struct {
	Bin  string
	Args []Arg
	Dir  string
}

func (c Command) Argv() []string {
	out := []string{}
	for _, a := range c.Args {
		out = append(out, a.Words()...)
	}
	return out
}

func (c Command) Line() string { return Shell(append([]string{c.Bin}, c.Argv()...)) }

func Shell(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = shellQuote(w)
	}
	return strings.Join(q, " ")
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const autoContextFloor = 32768

var kvBytes = map[string]float64{"f16": 2, "q8_0": 34.0 / 32, "q4_0": 18.0 / 32}

func Build(e Env, c Config) Command {
	m := e.Model
	cmd := Command{Bin: "llama-server"}
	if c.Mode == Chat {
		cmd.Bin = "llama-cli"
	}
	add := func(setting, flag, value, why string) {
		cmd.Args = append(cmd.Args, Arg{Flag: flag, Value: value, Why: why, Setting: setting})
	}

	add("", "-m", m.Path, "the model file")
	if c.Vision && m.MMProj != "" {
		add("vision", "--mmproj", m.MMProj, "the vision adapter: lets the model look at images")
	}

	if c.Context > 0 {
		add("context", "-c", itoa(c.Context), fmt.Sprintf("a fixed context window of %s tokens", kilo(c.Context)))
	} else {
		floor := autoContextFloor
		if mx := int(m.Info.ContextOr(0)); mx > 0 && mx < floor {
			floor = mx
		}
		add("context", "--fit-ctx", itoa(floor), fmt.Sprintf("context uses the model's maximum when memory allows, shrinking to fit but never below %s tokens", kilo(floor)))
	}
	switch {
	case c.Offload == "cpu":
		add("offload", "-dev", "none", "no GPU: everything runs on the CPU")
	case c.Offload == "all":
		add("offload", "-ngl", "all", "every layer on the GPU (fails to load if it doesn't fit)")
	}
	if c.Offload != "cpu" {
		if id := c.deviceID(e.HW); id != "" {
			add("device", "-dev", id, "only this GPU is used")
		}
	}
	if c.Experts == "cpu" && m.Info != nil && m.Info.Experts > 0 {
		add("experts", "--cpu-moe", "", "mixture-of-experts weights stay in RAM; only the always-active parts use the GPU")
	}

	if c.Mode == Serve {
		host, why := "127.0.0.1", "only this machine can connect"
		if c.Listen == "network" {
			host, why = "0.0.0.0", "anyone on your network can connect (there is no password)"
		}
		add("listen", "--host", host, why)
		add("port", "--port", itoa(c.Port), "where the web UI and API are served")
		if m.Kind() == gguf.Embedding {
			add("", "--embeddings", "", "this is an embedding model: it serves /v1/embeddings instead of chat")
		}
		if c.Parallel > 0 {
			add("parallel", "-np", itoa(c.Parallel), fmt.Sprintf("%d requests handled at once, each with 1/%d of the context", c.Parallel, c.Parallel))
		}
		if c.Tools {
			add("tools", "--tools", "all", "the model may read/write files and run shell commands as you")
		}
	}

	if c.KVCache != "f16" {
		add("kv", "-ctk", c.KVCache, "keys in the context cache are stored compressed")
		add("kv", "-ctv", c.KVCache, "values in the context cache are stored compressed")
	}
	if c.Flash != "auto" {
		add("flash", "-fa", c.Flash, "flash attention forced "+c.Flash)
	}
	if c.Threads > 0 {
		add("threads", "-t", itoa(c.Threads), "CPU threads used for the parts running on the CPU")
	}
	for _, w := range strings.Fields(c.Extra) {
		if strings.HasPrefix(w, "-") {
			add("extra", w, "", "your own flag")
		} else if n := len(cmd.Args); n > 0 && cmd.Args[n-1].Setting == "extra" && cmd.Args[n-1].Value == "" {
			cmd.Args[n-1].Value = w
		} else {
			add("extra", w, "", "your own flag")
		}
	}
	cmd.Dir, _ = os.Getwd()
	return cmd
}

func (c Config) deviceID(h hw.Hardware) string {
	if c.Device != "" {
		return c.Device
	}
	if d, ok := h.Primary(); ok {
		return d.ID
	}
	return ""
}

func kilo(n int) string {
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dk", n/1024)
	}
	return itoa(n)
}

type Mem struct {
	Weights int64
	KV      int64
	Context int
	Floor   bool
	GPU     int64
	Known   bool
}

func Estimate(e Env, c Config) Mem {
	mem := Mem{Weights: e.Model.Size, Context: c.Context}
	if c.Context == 0 {
		mem.Context, mem.Floor = autoContextFloor, true
		if mx := int(e.Model.Info.ContextOr(0)); mx > 0 && mx < mem.Context {
			mem.Context = mx
		}
	}
	if e.Model.Info != nil {
		if per := e.Model.Info.KVBytesPerToken(kvBytes[c.KVCache]); per > 0 {
			mem.KV, mem.Known = int64(per*float64(mem.Context)), true
		}
	}
	if c.Offload != "cpu" {
		mem.GPU = e.HW.VRAM(c.deviceID(e.HW))
	}
	return mem
}

type Note struct {
	Danger bool
	Text   string
}

func Notes(e Env, c Config) []Note {
	var out []Note
	add := func(danger bool, f string, a ...any) { out = append(out, Note{danger, fmt.Sprintf(f, a...)}) }

	if c.Mode == Serve && c.Tools {
		add(true, "Tools are on: the model can read and write files and run shell commands as you, in %s.", cwd())
		if c.Listen == "network" {
			add(true, "Tools plus network access: anyone on your network can make it do that.")
		}
	} else if c.Mode == Serve && c.Listen == "network" {
		add(false, "Network access: anyone on your network can use this server and your GPU. There is no password.")
	}
	if mem := Estimate(e, c); mem.GPU > 0 {
		need := mem.Weights + mem.KV
		switch hw.Assess(need, mem.GPU, e.HW.RAM) {
		case hw.Spills:
			if c.Offload == "all" {
				add(true, "Needs about %s but the GPU has %s: with all layers on the GPU this will fail to load.", gb(need), gib(mem.GPU))
			} else {
				add(false, "Needs about %s but the GPU has %s: part of the model will run on the CPU, which is slower.", gb(need), gib(mem.GPU))
			}
		case hw.TooBig:
			add(true, "Needs about %s: more than the GPU and free RAM together.", gb(need))
		}
	}
	if e.Model.Kind() == gguf.Embedding {
		add(false, "This is an embedding model: it turns text into vectors (/v1/embeddings) and can't chat.")
	}
	if c.KVCache != "f16" && c.Flash == "off" {
		add(true, "A compressed context cache needs flash attention; with it off llama.cpp will refuse to start.")
	}
	return out
}

func cwd() string {
	d, _ := os.Getwd()
	return d
}

func gib(n int64) string { return fmt.Sprintf("%.0f GB", float64(n)/hw.GiB) }

func gb(n int64) string { return fmt.Sprintf("%.1f GB", float64(n)/1e9) }
