package llama

import (
	"fmt"
	"strconv"
	"strings"

	"loom/internal/gguf"
)

type Choice struct {
	Value string
	Label string
	Help  string
}

type Setting struct {
	ID      string
	Group   string
	Label   string
	Help    string
	Text    bool
	Choices func(Env) []Choice
	Visible func(Env, Config) bool
	Get     func(Config) string
	Set     func(*Config, string) error
}

func (s Setting) IsVisible(e Env, c Config) bool { return s.Visible == nil || s.Visible(e, c) }

func Settings(e Env, c Config) []Setting {
	var out []Setting
	for _, s := range AllSettings {
		if s.IsVisible(e, c) {
			out = append(out, s)
		}
	}
	return out
}

func (s Setting) Display(e Env, c Config) string {
	v := s.Get(c)
	if s.Text {
		if v == "" {
			return "none"
		}
		return v
	}
	for _, ch := range s.Choices(e) {
		if ch.Value == v {
			return ch.Label
		}
	}
	return v
}

func (s Setting) Cycle(e Env, c *Config, delta int) {
	choices := s.Choices(e)
	cur := 0
	for i, ch := range choices {
		if ch.Value == s.Get(*c) {
			cur = i
		}
	}
	s.Set(c, choices[(cur+delta+len(choices))%len(choices)].Value)
}

func (s Setting) HelpFor(e Env, c Config) string {
	if !s.Text {
		for _, ch := range s.Choices(e) {
			if ch.Value == s.Get(c) && ch.Help != "" {
				return ch.Help
			}
		}
	}
	return s.Help
}

func serverOnly(_ Env, c Config) bool { return c.Mode == Serve }
func gpuOnly(_ Env, c Config) bool    { return c.Offload != "cpu" }

var AllSettings = []Setting{
	{
		ID: "mode", Group: "Run", Label: "Run as",
		Help: "How you talk to the model.",
		Choices: func(e Env) []Choice {
			serve := Choice{string(Serve), "Server", "Starts llama-server: a web chat UI and an OpenAI-compatible API at /v1, for apps and browsers."}
			if e.Model.Kind() == gguf.Embedding {
				return []Choice{serve}
			}
			return []Choice{serve, {string(Chat), "Terminal chat", "Starts llama-cli: chat right here in this terminal. Quit it to come back to loom."}}
		},
		Get: func(c Config) string { return string(c.Mode) },
		Set: func(c *Config, v string) error { c.Mode = Mode(v); return nil },
	},
	{
		ID: "context", Group: "Memory", Label: "Context window",
		Help: "How much text (prompt + conversation) the model can hold at once. Longer needs more memory.",
		Choices: func(e Env) []Choice {
			out := []Choice{{"0", "Auto", "Uses the model's maximum when memory allows, shrinking to fit, but never below 32k tokens (it moves layers to the CPU instead)."}}
			max := int(e.Model.Info.ContextOr(131072))
			for _, n := range []int{4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, 1048576} {
				if n <= max {
					out = append(out, Choice{itoa(n), kilo(n) + " tokens", "A fixed window of exactly this many tokens."})
				}
			}
			return out
		},
		Get: func(c Config) string { return itoa(c.Context) },
		Set: func(c *Config, v string) error {
			if v == "auto" {
				v = "0"
			}
			n, err := strconv.Atoi(v)
			c.Context = n
			return err
		},
	},
	{
		ID: "offload", Group: "Memory", Label: "GPU use",
		Help: "How much of the model is loaded into GPU memory. GPU is much faster than CPU.",
		Choices: func(Env) []Choice {
			return []Choice{
				{"auto", "Fit to memory", "llama.cpp puts as many layers on the GPU as fit, and runs the rest on the CPU. Best for most models."},
				{"all", "All on GPU", "Every layer on the GPU. Fastest, but the model fails to load if it doesn't fit."},
				{"cpu", "CPU only", "Nothing on the GPU. Slow, but leaves the GPU free for other things."},
			}
		},
		Get: func(c Config) string { return c.Offload },
		Set: func(c *Config, v string) error { c.Offload = v; return nil },
	},
	{
		ID: "device", Group: "Memory", Label: "Which GPU",
		Help:    "Your machine has more than one device llama.cpp can use.",
		Visible: func(e Env, c Config) bool { return len(e.HW.Devices) > 1 && gpuOnly(e, c) },
		Choices: func(e Env) []Choice {
			var out []Choice
			for i, d := range e.HW.Devices {
				help := fmt.Sprintf("%s, %.0f GB of memory.", d.Name, float64(d.Total)/(1<<30))
				label := fmt.Sprintf("%s (%.0f GB)", d.Short(), float64(d.Total)/(1<<30))
				if i == 0 {
					help += " The first device llama.cpp lists; usually the fastest."
					out = append(out, Choice{"", label, help})
				} else {
					out = append(out, Choice{d.ID, label, help})
				}
			}
			return out
		},
		Get: func(c Config) string { return c.Device },
		Set: func(c *Config, v string) error { c.Device = v; return nil },
	},
	{
		ID: "experts", Group: "Memory", Label: "Expert weights",
		Help:    "This is a mixture-of-experts model: only a few 'experts' are active per token.",
		Visible: func(e Env, c Config) bool { return e.Model.Info != nil && e.Model.Info.Experts > 0 && gpuOnly(e, c) },
		Choices: func(Env) []Choice {
			return []Choice{
				{"gpu", "With the rest", "Experts are placed on the GPU like any other layer."},
				{"cpu", "Keep in RAM", "Experts stay in system RAM and only the always-active parts use the GPU. Lets a big MoE model run well on a small GPU."},
			}
		},
		Get: func(c Config) string { return c.Experts },
		Set: func(c *Config, v string) error { c.Experts = v; return nil },
	},
	{
		ID: "vision", Group: "Memory", Label: "Image input",
		Help:    "This model has a vision adapter.",
		Visible: func(e Env, _ Config) bool { return e.Model.MMProj != "" },
		Choices: func(Env) []Choice {
			return []Choice{
				{"true", "On", "Loads the vision adapter so you can send images. Uses a little more memory."},
				{"false", "Off", "Text only; saves memory."},
			}
		},
		Get: func(c Config) string { return strconv.FormatBool(c.Vision) },
		Set: func(c *Config, v string) error { c.Vision = v == "true"; return nil },
	},
	{
		ID: "listen", Group: "Server", Label: "Who can connect",
		Help:    "The network interface the server listens on.",
		Visible: serverOnly,
		Choices: func(Env) []Choice {
			return []Choice{
				{"local", "Only this machine", "Listens on 127.0.0.1. Nobody else can reach it."},
				{"network", "Whole network", "Listens on 0.0.0.0, so other devices on your network can use it. There is no password."},
			}
		},
		Get: func(c Config) string { return c.Listen },
		Set: func(c *Config, v string) error { c.Listen = v; return nil },
	},
	{
		ID: "port", Group: "Server", Label: "Port", Text: true,
		Help:    "Where the server answers. If it's taken, loom uses the next free one.",
		Visible: serverOnly,
		Get:     func(c Config) string { return itoa(c.Port) },
		Set: func(c *Config, v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1024 || n > 65535 {
				return fmt.Errorf("a port between 1024 and 65535")
			}
			c.Port = n
			return nil
		},
	},
	{
		ID: "tools", Group: "Server", Label: "Agent tools",
		Help:    "llama-server's built-in tools let the model act on your machine.",
		Visible: serverOnly,
		Choices: func(Env) []Choice {
			return []Choice{
				{"false", "Off", "The model can only produce text."},
				{"true", "On", "The model can read and write files and run shell commands as you, in the folder you started loom from. Only for models and people you trust."},
			}
		},
		Get: func(c Config) string { return strconv.FormatBool(c.Tools) },
		Set: func(c *Config, v string) error { c.Tools = v == "true"; return nil },
	},
	{
		ID: "parallel", Group: "Server", Label: "Simultaneous chats",
		Help:    "How many requests the server works on at once. The context window is divided between them.",
		Visible: serverOnly,
		Choices: func(Env) []Choice {
			out := []Choice{{"0", "Auto", "llama.cpp decides."}}
			for _, n := range []int{1, 2, 4, 8} {
				out = append(out, Choice{itoa(n), itoa(n), ""})
			}
			return out
		},
		Get: func(c Config) string { return itoa(c.Parallel) },
		Set: func(c *Config, v string) error { n, err := strconv.Atoi(v); c.Parallel = n; return err },
	},
	{
		ID: "kv", Group: "Advanced", Label: "Context cache",
		Help: "The context window's working memory (KV cache). Compressing it frees memory for a longer context, at a small quality cost.",
		Choices: func(Env) []Choice {
			return []Choice{
				{"f16", "Full precision", "Default: best quality."},
				{"q8_0", "Compressed ½", "About half the memory, nearly no quality loss."},
				{"q4_0", "Compressed ¼", "About a quarter of the memory, with some quality loss."},
			}
		},
		Get: func(c Config) string { return c.KVCache },
		Set: func(c *Config, v string) error { c.KVCache = v; return nil },
	},
	{
		ID: "flash", Group: "Advanced", Label: "Flash attention",
		Help: "A faster, leaner way of computing attention.",
		Choices: func(Env) []Choice {
			return []Choice{
				{"auto", "Auto", "llama.cpp turns it on when the hardware supports it."},
				{"on", "On", "Force it on."},
				{"off", "Off", "Force it off. A compressed context cache needs it."},
			}
		},
		Get: func(c Config) string { return c.Flash },
		Set: func(c *Config, v string) error { c.Flash = v; return nil },
	},
	{
		ID: "threads", Group: "Advanced", Label: "CPU threads",
		Help: "Threads for the parts running on the CPU. Only matters when layers spill off the GPU.",
		Choices: func(Env) []Choice {
			out := []Choice{{"0", "Auto", "llama.cpp picks based on your CPU."}}
			for _, n := range []int{2, 4, 6, 8, 12, 16, 24, 32} {
				out = append(out, Choice{itoa(n), itoa(n), ""})
			}
			return out
		},
		Get: func(c Config) string { return itoa(c.Threads) },
		Set: func(c *Config, v string) error { n, err := strconv.Atoi(v); c.Threads = n; return err },
	},
	{
		ID: "extra", Group: "Advanced", Label: "Extra flags", Text: true,
		Help: "Anything else to pass to llama.cpp, e.g. --temp 0.6 --jinja. Appended last; run `llama-server --help` for the list.",
		Get:  func(c Config) string { return c.Extra.String() },
		Set: func(c *Config, v string) error {
			words, err := SplitArgs(v)
			if err == nil {
				c.Extra = words
			}
			return err
		},
	},
}

func SettingByID(id string) (Setting, bool) {
	for _, s := range AllSettings {
		if s.ID == id {
			return s, true
		}
	}
	return Setting{}, false
}
