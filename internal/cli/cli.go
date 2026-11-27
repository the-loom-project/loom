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
	"os/signal"
	"strings"
	"syscall"
	"time"

	"loom/internal/config"
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
	cfg, cerr := config.Load()
	profiles, perr := llama.LoadProfiles()
	st, hub, km := store.Open(), hf.New(cfg.NetTimeout()), keys.Load()
	var warnings []string
	for _, err := range []error{cerr, perr} {
		if err != nil {
			warnings = append(warnings, err.Error())
		}
	}
	var err error
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if page, ok := helpFor(cmd); ok && wantsHelp(args) {
		fmt.Print(page)
		return 0
	}
	if cmd != "" && cmd != "complete" {
		for _, w := range warnings {
			fmt.Fprintln(os.Stderr, "loom: warning:", w)
		}
	}
	switch cmd {
	case "":
		err = tui.Run(st, hub, profiles, km, cfg, warnings)
	case "run":
		err = run(st, hub, profiles, args, cfg.Limits())
	case "pull":
		err = pull(st, hub, args, cfg.Limits())
	case "ls", "list":
		list(st, cfg.Limits())
	case "rm":
		err = remove(st, args)
	case "keys":
		fmt.Print(km.Dump())
	case "completion":
		err = completion(args)
	case "complete":
		complete(st, profiles, args)
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

func list(st *store.Store, lim hw.Limits) {
	models, others := st.Scan()
	h := hw.Detect()
	if len(models)+len(others) == 0 {
		fmt.Println("No models yet. Run `loom` to browse and download one.")
		return
	}
	w, kw := 0, 0
	for _, m := range models {
		w, kw = max(w, len(m.Ref)), max(kw, len(kindOf(m)))
	}
	for _, o := range others {
		w = max(w, len(o.Repo))
	}
	for _, m := range models {
		ctx := "?"
		if m.Info != nil {
			ctx = units.Tokens(m.Info.Context)
		}
		fmt.Printf("%-*s  %8s  %-*s  %6s  %s\n", w, m.Ref, units.Bytes(m.Size), kw, kindOf(m), ctx, lim.Assess(m.Size, h.VRAM(""), h.RAM).Label())
	}
	for _, o := range others {
		fmt.Printf("%-*s  %8s  %s\n", w, o.Repo, units.Bytes(o.Size), o.Status())
	}
}

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
	if !yes && !ask(fmt.Sprintf("Delete %s (%s)?", m.Ref, units.Bytes(m.Size))) {
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

var errAmbiguous = errors.New("is ambiguous")

func resolve(models []store.Model, q string) (store.Model, error) {
	matches := store.Find(models, q)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return store.Model{}, fmt.Errorf("no downloaded model matches %q (see `loom ls`)", q)
	}
	var names []string
	for _, m := range matches {
		names = append(names, m.Ref)
	}
	return store.Model{}, fmt.Errorf("%q %w: %s", q, errAmbiguous, strings.Join(names, ", "))
}

func pull(st *store.Store, hub *hf.Client, args []string, lim hw.Limits) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: loom pull <owner/repo[:QUANT]>")
	}
	_, err := fetch(st, hub, args[0], lim)
	return err
}

func fetch(st *store.Store, hub *hf.Client, ref string, lim hw.Limits) (store.Model, error) {
	id, quant, ok := hf.ParseRef(ref)
	if !ok {
		return store.Model{}, fmt.Errorf("%q is not a model name or owner/repo", ref)
	}
	repo, err := hub.Repo(context.Background(), id)
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
	rec := lim.Pick(names, sizes, h.VRAM(""))
	if quant == "" {
		if len(quants) == 1 {
			quant, rec = quants[0].Name, 0
		} else {
			printQuants(id, quants, rec, h, lim)
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
		printQuants(id, quants, rec, h, lim)
		return store.Model{}, fmt.Errorf("%s has no %s", id, quant)
	}

	models, _ := st.Scan()
	if m, ok := store.Lookup(models, id, quant); ok {
		fmt.Printf("✓ %s:%s is already downloaded\n", id, quant)
		return m, nil
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
		fmt.Fprintf(os.Stderr, "\r  %3d%%  %s / %s  %s/s%s   ", 100*p.Done/max(1, p.Total), units.Bytes(p.Done), units.Bytes(p.Total), units.Bytes(int64(p.Speed)), eta)
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return store.Model{}, err
	}
	models, _ = st.Scan()
	fmt.Printf("✓ downloaded %s:%s\n", id, quant)
	m, _ := store.Lookup(models, id, quant)
	return m, nil
}

func printQuants(id string, quants []hf.Quant, rec int, h hw.Hardware, lim hw.Limits) {
	fmt.Printf("%s: pick a quantization (bigger = closer to the original, slower, more memory):\n\n", id)
	for i, q := range quants {
		mark := ""
		if i == rec {
			mark = "  ← recommended"
		}
		fmt.Printf("  %-14s %8s  %-14s %s%s\n", q.Label(), units.Bytes(q.Size), lim.Assess(q.Size, h.VRAM(""), h.RAM).Label(), gguf.QuantNote(q.Name), mark)
	}
	fmt.Println()
}

func run(st *store.Store, hub *hf.Client, profiles *llama.Profiles, args []string, lim hw.Limits) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	chat := fs.Bool("chat", false, "")
	lan := fs.Bool("lan", false, "")
	tools := fs.Bool("tools", false, "")
	cpu := fs.Bool("cpu", false, "")
	ctx := fs.Int("ctx", 0, "")
	port := fs.Int("port", 0, "")
	profile := fs.String("profile", "", "")
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
		if _, _, isRef := hf.ParseRef(name); !isRef || errors.Is(err, errAmbiguous) {
			return err
		}
		if m, err = fetch(st, hub, name, lim); err != nil {
			return err
		}
	}
	h := hw.Detect()
	if !h.Installed {
		return fmt.Errorf("llama.cpp isn't installed: sudo pacman -S llama-cpp ggml-vulkan")
	}
	env := llama.Env{Model: m, HW: h, Limits: lim}
	cfg := profiles.Get(env)
	if *profile != "" {
		saved, ok := profiles.Profile(m.Ref, *profile)
		if !ok {
			return fmt.Errorf("%s has no profile %q (saved: %s)", m.Ref, *profile, strings.Join(profiles.Names(m.Ref), ", "))
		}
		cfg = saved
	}
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
		cfg.Extra = extra
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

	fmt.Printf("%s  %s\n\n%s\n\n", m.Ref, units.Bytes(m.Size), tui.Explain(cmd, 100))
	for _, n := range llama.Notes(env, cfg) {
		fmt.Println("! " + n.Text)
	}
	if cfg.Mode == llama.Serve {
		fmt.Printf("\nServing on http://127.0.0.1:%d (web UI; OpenAI API at /v1). Ctrl-C to stop.\n\n", cfg.Port)
	}
	return foreground(cmd)
}

func foreground(c llama.Command) error {
	signal.Ignore(os.Interrupt)
	cmd := c.Exec()
	if err := cmd.Start(); err != nil {
		return err
	}
	forward := make(chan os.Signal, 1)
	signal.Notify(forward, syscall.SIGTERM, syscall.SIGHUP)
	defer func() {
		signal.Stop(forward)
		close(forward)
	}()
	go func() {
		for sig := range forward {
			cmd.Process.Signal(sig)
		}
	}()
	err := cmd.Wait()
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
