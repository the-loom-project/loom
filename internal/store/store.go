package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"loom/internal/gguf"
	"loom/internal/hf"
)

type Source int

const (
	HF Source = iota
	Local
)

type Model struct {
	Ref    string
	Repo   string
	Quant  string
	Source Source
	Path   string
	MMProj string
	Size   int64
	Info   *gguf.Info
	Err    error

	Revision string
	Blobs    map[string]string
	Meta     hf.Meta
	HasMeta  bool

	files []string
}

func (m Model) Kind() gguf.Kind {
	if m.Info == nil {
		return gguf.NoChat
	}
	return m.Info.Kind()
}

func (m Model) Name() string {
	if m.Repo != "" {
		return strings.TrimSuffix(m.Repo[strings.Index(m.Repo, "/")+1:], "-GGUF")
	}
	return m.Ref
}

type Other struct {
	Repo string
	Size int64
	dir  string
}

type Store struct {
	Root string
}

func Open() *Store {
	root := os.Getenv("LOOM_MODELS")
	if root == "" {
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			home, _ := os.UserHomeDir()
			data = filepath.Join(home, ".local", "share")
		}
		root = filepath.Join(data, "loom", "models")
	}
	return &Store{Root: root}
}

func (s *Store) Hub() string      { return filepath.Join(s.Root, "hf", "hub") }
func (s *Store) LocalDir() string { return filepath.Join(s.Root, "local") }

func (s *Store) Scan() ([]Model, []Other) {
	var models []Model
	var others []Other

	entries, _ := os.ReadDir(s.Hub())
	for _, e := range entries {
		name, ok := strings.CutPrefix(e.Name(), "models--")
		if !ok || !e.IsDir() {
			continue
		}
		repo := strings.Replace(name, "--", "/", 1)
		dir := filepath.Join(s.Hub(), e.Name())
		found := scanRepo(repo, dir)
		if len(found) > 0 {
			models = append(models, found...)
		} else if size := dirSize(filepath.Join(dir, "snapshots")); size > 0 {
			others = append(others, Other{Repo: repo, Size: size, dir: dir})
		}
	}

	locals, _ := filepath.Glob(filepath.Join(s.LocalDir(), "*.gguf"))
	for _, p := range locals {
		if strings.HasSuffix(p, ".mmproj.gguf") {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(p), ".gguf")
		m := Model{Ref: name, Source: Local, Path: p, Size: fileSize(p), files: []string{p}}
		if mm := filepath.Join(s.LocalDir(), name+".mmproj.gguf"); fileSize(mm) > 0 {
			m.MMProj = mm
			m.files = append(m.files, mm)
		}
		m.Info, m.Err = gguf.Read(p)
		models = append(models, m)
	}

	sort.Slice(models, func(i, j int) bool {
		if models[i].Source != models[j].Source {
			return models[i].Source > models[j].Source
		}
		return strings.ToLower(models[i].Ref) < strings.ToLower(models[j].Ref)
	})
	sort.Slice(others, func(i, j int) bool { return others[i].Repo < others[j].Repo })
	return models, others
}

func scanRepo(repo, dir string) []Model {
	rev, err := os.ReadFile(filepath.Join(dir, "refs", "main"))
	if err != nil {
		return nil
	}
	revision := strings.TrimSpace(string(rev))
	snap := filepath.Join(dir, "snapshots", revision)
	byQuant := map[string][]string{}
	var mmproj string
	filepath.WalkDir(snap, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".gguf") {
			return nil
		}
		rel, _ := filepath.Rel(snap, p)
		if gguf.IsMMProj(rel) {
			if mmproj == "" || strings.Contains(strings.ToUpper(rel), "F16") {
				mmproj = p
			}
			return nil
		}
		q := gguf.QuantOf(rel)
		byQuant[q] = append(byQuant[q], p)
		return nil
	})

	var out []Model
	for q, files := range byQuant {
		sort.Strings(files)
		m := Model{Repo: repo, Quant: q, Source: HF, Path: files[0], MMProj: mmproj, files: files, Revision: revision, Blobs: map[string]string{}}
		m.Meta, m.HasMeta = hf.ReadMeta(filepath.Dir(dir), repo)
		for _, f := range append(append([]string{}, files...), mmproj) {
			if f == "" {
				continue
			}
			if blob, err := filepath.EvalSymlinks(f); err == nil {
				rel, _ := filepath.Rel(snap, f)
				m.Blobs[filepath.ToSlash(rel)] = filepath.Base(blob)
			}
		}
		m.Ref = repo
		if q != "" {
			m.Ref += ":" + q
		}
		for _, f := range files {
			m.Size += fileSize(f)
		}
		if mmproj != "" {
			m.files = append(append([]string{}, files...), mmproj)
		}
		m.Info, m.Err = gguf.Read(files[0])
		if m.Info != nil {
			for _, shard := range files[1:] {
				if s, err := gguf.Read(shard); err == nil {
					m.Info.Params += s.Params
				}
			}
		}
		out = append(out, m)
	}
	return out
}

func (s *Store) Remove(m Model) error {
	if m.Source == Local {
		var err error
		for _, f := range m.files {
			err = errors.Join(err, os.Remove(f))
		}
		return err
	}
	dir := hf.RepoDir(s.Hub(), m.Repo)
	keep := map[string]bool{}
	siblings := 0
	for _, o := range scanRepo(m.Repo, dir) {
		if o.Quant == m.Quant {
			continue
		}
		siblings++
		for _, f := range o.files {
			keep[f] = true
		}
	}
	if siblings == 0 {
		return os.RemoveAll(dir)
	}
	var err error
	for _, f := range m.files {
		if keep[f] {
			continue
		}
		blob, _ := filepath.EvalSymlinks(f)
		err = errors.Join(err, os.Remove(f))
		if blob != "" && !linked(dir, blob) {
			os.Remove(blob)
		}
	}
	prune(dir)
	return err
}

func linked(dir, blob string) bool {
	found := false
	filepath.WalkDir(filepath.Join(dir, "snapshots"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if t, e := filepath.EvalSymlinks(p); e == nil && t == blob {
				found = true
			}
		}
		return nil
	})
	return found
}

func (s *Store) RemoveOther(o Other) error { return os.RemoveAll(o.dir) }

func Has(models []Model, repo, quant string) bool {
	for _, m := range models {
		if m.Repo == repo && m.Quant == quant {
			return true
		}
	}
	return false
}

func fileSize(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n += fileSize(p)
		}
		return nil
	})
	return n
}

func prune(dir string) {
	blobs := filepath.Join(dir, "blobs")
	entries, _ := os.ReadDir(blobs)
	for _, e := range entries {
		p := filepath.Join(blobs, e.Name())
		if _, err := os.Stat(p); err != nil {
			os.Remove(p)
		} else if e.IsDir() {
			os.Remove(p)
		}
	}
}

func Find(models []Model, q string) (Model, []Model) {
	q = strings.ToLower(q)
	var partial []Model
	for _, m := range models {
		if strings.ToLower(m.Ref) == q || strings.ToLower(m.Name()) == q {
			return m, nil
		}
		if strings.Contains(strings.ToLower(m.Ref), q) {
			partial = append(partial, m)
		}
	}
	if len(partial) == 1 {
		return partial[0], nil
	}
	return Model{}, partial
}
