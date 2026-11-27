package hf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Progress struct {
	File  string
	Done  int64
	Total int64
	Speed float64
}

func RepoDir(hub, repo string) string {
	return filepath.Join(hub, "models--"+strings.ReplaceAll(repo, "/", "--"))
}

func (c *Client) Download(ctx context.Context, hub, repo, sha string, files []File, report func(Progress)) error {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	dir := RepoDir(hub, repo)
	prog := &meter{total: total, report: report, last: time.Now()}
	for _, f := range files {
		prog.file = f.Path
		if err := c.fetch(ctx, dir, repo, sha, f, prog); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o755); err != nil {
		return err
	}
	carryOver(dir, sha)
	return os.WriteFile(filepath.Join(dir, "refs", "main"), []byte(sha), 0o644)
}

func carryOver(dir, sha string) {
	old, err := os.ReadFile(filepath.Join(dir, "refs", "main"))
	if err != nil || strings.TrimSpace(string(old)) == sha {
		return
	}
	from := filepath.Join(dir, "snapshots", strings.TrimSpace(string(old)))
	to := filepath.Join(dir, "snapshots", sha)
	filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(from, p)
		link := filepath.Join(to, rel)
		if _, err := os.Lstat(link); err == nil {
			return nil
		}
		if target, err := os.Readlink(p); err == nil {
			os.MkdirAll(filepath.Dir(link), 0o755)
			os.Symlink(target, link)
		}
		return nil
	})
}

func (c *Client) Discard(hub, repo string, files []File) error {
	dir := RepoDir(hub, repo)
	var errs []error
	for _, f := range files {
		blob := filepath.Join(dir, "blobs", f.BlobID)
		unlock, _ := c.lock(context.Background(), blob)
		if err := os.Remove(blob + ".incomplete"); !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
		unlock()
	}
	return errors.Join(errs...)
}

func Clean(hub, repo string) error {
	dir := RepoDir(hub, repo)
	if _, err := os.Stat(filepath.Join(dir, "refs", "main")); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	parts, _ := filepath.Glob(filepath.Join(dir, "blobs", "*.incomplete"))
	if len(parts) > 0 {
		return nil
	}
	return os.RemoveAll(dir)
}

func Prune(hub, repo, keep string) error {
	dir := RepoDir(hub, repo)
	var errs []error
	snaps, _ := os.ReadDir(filepath.Join(dir, "snapshots"))
	for _, s := range snaps {
		if s.Name() != keep {
			errs = append(errs, os.RemoveAll(filepath.Join(dir, "snapshots", s.Name())))
		}
	}
	used := map[string]bool{}
	var dirs []string
	filepath.WalkDir(filepath.Join(dir, "snapshots", keep), func(p string, d os.DirEntry, err error) error {
		switch {
		case err != nil:
		case d.IsDir():
			dirs = append(dirs, p)
		default:
			if t, err := filepath.EvalSymlinks(p); err == nil {
				used[filepath.Base(t)] = true
			} else {
				errs = append(errs, os.Remove(p))
			}
		}
		return nil
	})
	for _, d := range slices.Backward(dirs) {
		os.Remove(d)
	}
	blobs, _ := os.ReadDir(filepath.Join(dir, "blobs"))
	for _, b := range blobs {
		if !used[b.Name()] && !strings.HasSuffix(b.Name(), ".incomplete") {
			errs = append(errs, os.Remove(filepath.Join(dir, "blobs", b.Name())))
		}
	}
	return errors.Join(errs...)
}

type Meta struct {
	Tags    []string  `json:"tags"`
	Gated   bool      `json:"gated"`
	Updated time.Time `json:"updated"`
}

func ReadMeta(hub, repo string) (Meta, bool) {
	var m Meta
	b, err := os.ReadFile(filepath.Join(RepoDir(hub, repo), "loom.json"))
	if err != nil || json.Unmarshal(b, &m) != nil {
		return Meta{}, false
	}
	return m, true
}

func WriteMeta(hub, repo string, m Meta) {
	if b, err := json.MarshalIndent(m, "", "  "); err == nil {
		os.WriteFile(filepath.Join(RepoDir(hub, repo), "loom.json"), b, 0o644)
	}
}

func (c *Client) lock(ctx context.Context, key string) (func(), error) {
	for {
		c.mu.Lock()
		held, busy := c.busy[key]
		if !busy {
			done := make(chan struct{})
			c.busy[key] = done
			c.mu.Unlock()
			return func() {
				c.mu.Lock()
				delete(c.busy, key)
				c.mu.Unlock()
				close(done)
			}, nil
		}
		c.mu.Unlock()
		select {
		case <-held:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *Client) fetch(ctx context.Context, dir, repo, sha string, f File, prog *meter) error {
	blob := filepath.Join(dir, "blobs", f.BlobID)
	link := filepath.Join(dir, "snapshots", sha, filepath.FromSlash(f.Path))
	unlock, err := c.lock(ctx, blob)
	if err != nil {
		return err
	}
	defer unlock()
	if st, err := os.Stat(blob); err == nil && st.Size() == f.Size {
		prog.skip(f.Size)
		return linkSnapshot(blob, link)
	}
	if err := os.MkdirAll(filepath.Dir(blob), 0o755); err != nil {
		return err
	}
	part := blob + ".incomplete"
	have := int64(-1)
	if st, err := os.Stat(part); err == nil && st.Size() <= f.Size {
		have = st.Size()
	}
	if have < f.Size {
		url := fmt.Sprintf("%s/%s/resolve/%s/%s", base, repo, sha, f.Path)
		if err := c.receive(ctx, url, part, max(have, 0), prog); err != nil {
			return err
		}
	} else {
		prog.skip(have)
	}
	if f.SHA256 != "" {
		if err := verify(part, f.SHA256); err != nil {
			os.Remove(part)
			return err
		}
	}
	if err := os.Rename(part, blob); err != nil {
		return err
	}
	return linkSnapshot(blob, link)
}

var errStalled = errors.New("the download stalled")

type kick struct {
	t *time.Timer
	d time.Duration
}

func (k kick) Write(p []byte) (int, error) {
	k.t.Reset(k.d)
	return len(p), nil
}

func (c *Client) receive(ctx context.Context, url, part string, have int64, prog *meter) (err error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stall := c.Timeout()
	timer := time.AfterFunc(stall, func() { cancel(fmt.Errorf("%w: nothing arrived for %s", errStalled, stall)) })
	defer timer.Stop()
	defer func() {
		if cause := context.Cause(ctx); errors.Is(cause, errStalled) {
			err = cause
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := statusError(resp); err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if resp.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		have = 0
	}
	out, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	prog.skip(have)
	_, err = io.Copy(out, io.TeeReader(resp.Body, io.MultiWriter(prog, kick{timer, stall})))
	return errors.Join(err, out.Close())
}

func linkSnapshot(blob, link string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	rel, err := filepath.Rel(filepath.Dir(link), blob)
	if err != nil {
		return err
	}
	os.Remove(link)
	return os.Symlink(rel, link)
}

func verify(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch (got %.12s…, want %.12s…): download deleted, try again", got, want)
	}
	return nil
}

type meter struct {
	file   string
	done   int64
	total  int64
	report func(Progress)
	last   time.Time
	lastN  int64
	speed  float64
}

func (m *meter) skip(n int64) { m.done += n }

func (m *meter) Write(p []byte) (int, error) {
	m.done += int64(len(p))
	m.lastN += int64(len(p))
	if dt := time.Since(m.last); dt >= 200*time.Millisecond {
		inst := float64(m.lastN) / dt.Seconds()
		if m.speed == 0 {
			m.speed = inst
		}
		m.speed = 0.8*m.speed + 0.2*inst
		m.last, m.lastN = time.Now(), 0
		m.report(Progress{File: m.file, Done: m.done, Total: m.total, Speed: m.speed})
	}
	return len(p), nil
}
