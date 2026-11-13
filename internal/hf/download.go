package hf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	return os.WriteFile(filepath.Join(dir, "refs", "main"), []byte(sha), 0o644)
}

func (c *Client) fetch(ctx context.Context, dir, repo, sha string, f File, prog *meter) error {
	blob := filepath.Join(dir, "blobs", f.BlobID)
	link := filepath.Join(dir, "snapshots", sha, filepath.FromSlash(f.Path))
	if st, err := os.Stat(blob); err == nil && st.Size() == f.Size {
		prog.add(f.Size)
		return linkSnapshot(blob, link)
	}
	if err := os.MkdirAll(filepath.Dir(blob), 0o755); err != nil {
		return err
	}
	part := blob + ".incomplete"
	have := int64(0)
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	if have > f.Size {
		have = 0
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%s/resolve/%s/%s", base, repo, sha, f.Path), nil)
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
	prog.add(have)
	_, err = io.Copy(out, io.TeeReader(resp.Body, prog))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
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

func (m *meter) add(n int64) { m.done += n; m.lastN += n }

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
