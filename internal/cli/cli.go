package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
	"time"

	"loom/internal/gguf"
	"loom/internal/hf"
	"loom/internal/hw"
	"loom/internal/keys"
	"loom/internal/llama"
	"loom/internal/store"
	"loom/internal/tui"
	"loom/internal/units"
)

func Main(args []string) int {
	st, hub, prefs, km := store.Open(), hf.New(), llama.LoadPrefs(), keys.Load()
	var err error
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if page, ok := helpFor(cmd); ok && wantsHelp(args) {
		fmt.Print(page)
		return 0
	}
	switch cmd {
	case "":
		err = tui.Run(st, hub, prefs, km)
	case "run":
		err = run(st, hub, prefs, args)
	case "pull":
		err = pull(st, hub, args)
	case "ls", "list":
		list(st)
	case "rm":
		err = remove(st, args)
	case "keys":
		fmt.Print(km.Dump())
	case "completion":
		err = completion(args)
	case "complete":
		complete(st, args)
	case "help", "-h", "--help":
		if page, ok := helpFor(strings.Join(args, "")); ok {
			fmt.Print(page)
		} else {
			fmt.Print(helpText())
		}
	default:
		fmt.Fprintf(os.Stderr, "loom: unknown command %q\n\n%s", cmd, helpText())
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loom:", err)
		return 1
	}
	return 0
}

func list(st *store.Store) {
	models, others := st.Scan()
	h := hw.Detect()
	if len(models)+len(others) == 0 {
		fmt.Println("No models yet. Run `loom` to browse and download one.")
		return
	}
	w := 0
	for _, m := range models {
		w = max(w, len(m.Ref))
	}
	for _, m := range models {
		ctx := "?"
		if m.Info != nil {
			ctx = fmt.Sprintf("%dk", m.Info.Context/1024)
		}
		kind := m.Kind().String()
		if m.MMProj != "" {
			kind += "+vision"
		}
		fmt.Printf("%-*s  %8s  %-16s  %6s  %s\n", w, m.Ref, size(m.Size), kind, ctx, hw.Assess(m.Size, h.VRAM(""), h.RAM).Label())
	}
	for _, o := range others {
		fmt.Printf("%-*s  %8s  not GGUF: llama.cpp can't run it\n", w, o.Repo, size(o.Size))
	}
}

func size(n int64) string { return units.Bytes(n) }

func remove(st *store.Store, args []string) error {
	yes := false
	var names []string
	for _, a := range args {
		if a == "-y" || a == "--yes" {
			yes = true
		} else {
			names = append(names, a)
		}
	}
	if len(names) != 1 {
		return fmt.Errorf("usage: loom rm [-y] <model>")
	}
	models, _ := st.Scan()
	m, err := resolve(models, names[0])
	if err != nil {
		return err
	}
	if !yes && !ask(fmt.Sprintf("Delete %s (%s)?", m.Ref, size(m.Size))) {
		return fmt.Errorf("aborted")
	}
	if err := st.Remove(m); err != nil {
		return err
	}
	fmt.Printf("✓ deleted %s\n", m.Ref)
	return nil
}

func ask(q string) bool {
	fmt.Printf("%s [y/N] ", q)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
}

func resolve(models []store.Model, q string) (store.Model, error) {
	m, candidates := store.Find(models, q)
	switch {
	case m.Path != "":
		return m, nil
	case len(candidates) == 0:
		return m, fmt.Errorf("no downloaded model matches %q (see `loom ls`)", q)
	}
	var names []string
	for _, c := range candidates {
		names = append(names, c.Ref)
	}
	return m, fmt.Errorf("%q is ambiguous: %s", q, strings.Join(names, ", "))
}

func pull(st *store.Store, hub *hf.Client, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: loom pull <owner/repo[:QUANT]>")
	}
	_, err := fetch(st, hub, args[0])
	return err
}

func fetch(st *store.Store, hub *hf.Client, ref string) (store.Model, error) {
	id, quant, ok := hf.ParseRef(ref)
	if !ok {
		return store.Model{}, fmt.Errorf("%q is not a model name or owner/repo", ref)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	repo, err := hub.Repo(ctx, id)
	cancel()
	if err != nil {
		return store.Model{}, err
	}
	quants := repo.Quants()
	if len(quants) == 0 {
		return store.Model{}, fmt.Errorf("%s has no GGUF files, so llama.cpp can't run it", id)
	}
	h := hw.Detect()
	names, sizes := make([]string, len(quants)), make([]int64, len(quants))
	for i, q := range quants {
		names[i], sizes[i] = q.Name, q.Size
	}
	rec := hw.Pick(names, sizes, h.VRAM(""))
	if quant == "" {
		if len(quants) == 1 {
			quant, rec = quants[0].Name, 0
		} else {
			printQuants(id, quants, rec, h)
			return store.Model{}, fmt.Errorf("pick a quant: loom pull %s:%s", id, quants[rec].Name)
		}
	}
	var q *hf.Quant
	for i := range quants {
		if quants[i].Name == quant {
			q = &quants[i]
		}
	}
	if q == nil {
		printQuants(id, quants, rec, h)
		return store.Model{}, fmt.Errorf("%s has no %s", id, quant)
	}

	if models, _ := st.Scan(); store.Has(models, id, quant) {
		fmt.Printf("✓ %s:%s is already downloaded\n", id, quant)
		return findRef(models, id, quant), nil
	}
	files := append([]hf.File{}, q.Files...)
	if mm := repo.MMProj(); mm != nil {
		files = append(files, *mm)
	}
	fmt.Printf("Downloading %s:%s into %s\n", id, quant, st.Hub())
	err = hub.Download(context.Background(), st.Hub(), id, repo.SHA, files, func(p hf.Progress) {
		eta := ""
		if p.Speed > 1 {
			eta = "  ETA " + (time.Duration(float64(p.Total-p.Done)/p.Speed) * time.Second).Round(time.Second).String()
		}
		fmt.Fprintf(os.Stderr, "\r  %3d%%  %s / %s  %s/s%s   ", 100*p.Done/max(1, p.Total), size(p.Done), size(p.Total), size(int64(p.Speed)), eta)
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return store.Model{}, err
	}
	models, _ := st.Scan()
	fmt.Printf("✓ downloaded %s:%s\n", id, quant)
	return findRef(models, id, quant), nil
}

func findRef(models []store.Model, id, quant string) store.Model {
	for _, m := range models {
		if m.Repo == id && m.Quant == quant {
			return m
		}
	}
	return store.Model{}
}

func printQuants(id string, quants []hf.Quant, rec int, h hw.Hardware) {
	fmt.Printf("%s: pick a quantization (bigger = closer to the original, slower, more memory):\n\n", id)
	for i, q := range quants {
		mark := ""
		if i == rec {
			mark = "  ← recommended"
		}
		fmt.Printf("  %-14s %8s  %-14s %s%s\n", q.Name, size(q.Size), hw.Assess(q.Size, h.VRAM(""), h.RAM).Label(), gguf.QuantNote(q.Name), mark)
	}
	fmt.Println()
}

func run(st *store.Store, hub *hf.Client, prefs *llama.Prefs, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	chat := fs.Bool("chat", false, "")
	lan := fs.Bool("lan", false, "")
	tools := fs.Bool("tools", false, "")
	cpu := fs.Bool("cpu", false, "")
	ctx := fs.Int("ctx", 0, "")
	port := fs.Int("port", 0, "")
	var sets setFlags
	fs.Var(&sets, "set", "")

	name, rest := "", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, rest = args[0], args[1:]
	}
	if err := fs.Parse(rest); err != nil || name == "" {
		return fmt.Errorf("usage: loom run <model> [options] (see `loom help`)")
	}

	models, _ := st.Scan()
	m, err := resolve(models, name)
	if err != nil {
		if _, _, isRef := hf.ParseRef(name); !isRef || strings.Contains(err.Error(), "ambiguous") {
			return err
		}
		if m, err = fetch(st, hub, name); err != nil {
			return err
		}
	}
	h := hw.Detect()
	if !h.Installed {
		return fmt.Errorf("llama.cpp isn't installed: sudo pacman -S llama-cpp ggml-vulkan")
	}
	env := llama.Env{Model: m, HW: h}
	cfg := prefs.Get(env)
	if *chat {
		cfg.Mode = llama.Chat
	}
	if *lan {
		cfg.Listen = "network"
	}
	if *tools {
		cfg.Tools = true
	}
	if *cpu {
		cfg.Offload = "cpu"
	}
	if *ctx > 0 {
		cfg.Context = *ctx
	}
	if *port > 0 {
		cfg.Port = *port
	}
	for _, kv := range sets {
		id, value, _ := strings.Cut(kv, "=")
		st, ok := llama.SettingByID(id)
		if !ok {
			return fmt.Errorf("--set: no setting %q (see `loom help`)", id)
		}
		if err := st.Set(&cfg, value); err != nil {
			return fmt.Errorf("--set %s: %w", id, err)
		}
	}
	if extra := fs.Args(); len(extra) > 0 {
		cfg.Extra = strings.TrimSpace(cfg.Extra + " " + strings.Join(extra, " "))
	}
	cfg = cfg.Fix(env)
	if cfg.Mode == llama.Serve {
		host := "127.0.0.1"
		if cfg.Listen == "network" {
			host = "0.0.0.0"
		}
		cfg.Port = llama.FreePort(host, cfg.Port)
	}
	cmd := llama.Build(env, cfg)

	fmt.Printf("%s  %s\n\n%s\n\n", m.Ref, size(m.Size), tui.Explain(cmd, 100))
	for _, n := range llama.Notes(env, cfg) {
		fmt.Println("! " + n.Text)
	}
	if cfg.Mode == llama.Serve {
		fmt.Printf("\nServing on http://127.0.0.1:%d (web UI; OpenAI API at /v1). Ctrl-C to stop.\n\n", cfg.Port)
	}
	return foreground(cmd)
}

func foreground(c llama.Command) error {
	err := c.Exec().Run()
	var exit *osexec.ExitError
	if errors.As(err, &exit) {
		if code := exit.ExitCode(); code > 0 && code != 130 {
			return fmt.Errorf("%s exited with status %d", c.Bin, code)
		}
		return nil
	}
	return err
}

func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
		if a == "--" {
			return false
		}
	}
	return false
}

type setFlags []string

func (s *setFlags) String() string     { return strings.Join(*s, ",") }
func (s *setFlags) Set(v string) error { *s = append(*s, v); return nil }
