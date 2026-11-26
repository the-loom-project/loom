package hf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"loom/internal/gguf"
)

const base = "https://huggingface.co"

type Client struct {
	http  *http.Client
	token string
}

func New() *Client {
	return &Client{http: &http.Client{}, token: findToken()}
}

func (c *Client) HasToken() bool { return c.token != "" }

func findToken() string {
	if t := os.Getenv("HF_TOKEN"); t != "" {
		return t
	}
	var paths []string
	if dir := os.Getenv("HF_HOME"); dir != "" {
		paths = append(paths, filepath.Join(dir, "token"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".cache", "huggingface", "token"))
	}
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

type gate bool

func (g *gate) UnmarshalJSON(b []byte) error {
	switch strings.TrimSpace(string(b)) {
	case "", "false", "null", `""`:
		*g = false
	default:
		*g = true
	}
	return nil
}

func (c *Client) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := statusError(resp); err != nil {
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func statusError(resp *http.Response) error {
	switch {
	case resp.StatusCode < 400:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("not found on Hugging Face")
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("gated or private: accept its terms on huggingface.co and set HF_TOKEN")
	}
	return fmt.Errorf("the Hub answered %s", resp.Status)
}

type Summary struct {
	ID        string    `json:"id"`
	Downloads int       `json:"downloads"`
	Likes     int       `json:"likes"`
	Updated   time.Time `json:"lastModified"`
	Tags      []string  `json:"tags"`
	Pipeline  string    `json:"pipeline_tag"`
	Gated     gate      `json:"gated"`
	GGUF      struct {
		Params  uint64 `json:"total"`
		Arch    string `json:"architecture"`
		Context uint64 `json:"context_length"`
	} `json:"gguf"`
}

func (c *Client) Search(ctx context.Context, query, task string, everything bool) ([]Summary, error) {
	q := url.Values{"limit": {"40"},
		"expand[]": {"downloads", "likes", "lastModified", "tags", "pipeline_tag", "gated", "gguf"}}
	if !everything {
		q.Set("filter", "gguf")
	}
	if task != "" {
		q.Set("pipeline_tag", task)
	}
	if strings.TrimSpace(query) == "" {
		q.Set("sort", "trendingScore")
	} else {
		q.Set("search", query)
		q.Set("sort", "downloads")
		q.Set("direction", "-1")
	}
	var out []Summary
	err := c.get(ctx, base+"/api/models?"+q.Encode(), &out)
	return out, err
}

type File struct {
	Path   string
	Size   int64
	BlobID string
	SHA256 string
}

type Repo struct {
	ID        string
	SHA       string
	Downloads int
	Likes     int
	Gated     bool
	Tags      []string
	Pipeline  string
	Params    uint64
	Arch      string
	Context   uint64
	Files     []File
}

func (c *Client) Repo(ctx context.Context, id string) (*Repo, error) {
	var raw struct {
		ID        string   `json:"id"`
		SHA       string   `json:"sha"`
		Downloads int      `json:"downloads"`
		Likes     int      `json:"likes"`
		Gated     gate     `json:"gated"`
		Tags      []string `json:"tags"`
		Pipeline  string   `json:"pipeline_tag"`
		GGUF      struct {
			Params  uint64 `json:"total"`
			Arch    string `json:"architecture"`
			Context uint64 `json:"context_length"`
		} `json:"gguf"`
		Siblings []struct {
			Name   string `json:"rfilename"`
			Size   int64  `json:"size"`
			BlobID string `json:"blobId"`
			LFS    *struct {
				SHA256 string `json:"sha256"`
			} `json:"lfs"`
		} `json:"siblings"`
	}
	if err := c.get(ctx, base+"/api/models/"+id+"?blobs=true", &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	r := &Repo{ID: raw.ID, SHA: raw.SHA, Downloads: raw.Downloads, Likes: raw.Likes, Tags: raw.Tags, Pipeline: raw.Pipeline, Params: raw.GGUF.Params, Arch: raw.GGUF.Arch, Context: raw.GGUF.Context, Gated: bool(raw.Gated)}
	for _, s := range raw.Siblings {
		f := File{Path: s.Name, Size: s.Size, BlobID: s.BlobID}
		if s.LFS != nil {
			f.SHA256, f.BlobID = s.LFS.SHA256, s.LFS.SHA256
		}
		r.Files = append(r.Files, f)
	}
	return r, nil
}

type Quant struct {
	Name  string
	Size  int64
	Files []File
}

func (r *Repo) Quants() []Quant {
	by := map[string]*Quant{}
	for _, f := range r.Files {
		if !gguf.IsWeights(f.Path) {
			continue
		}
		name := gguf.QuantOf(f.Path)
		if name == "" {
			name = "?"
		}
		q := by[name]
		if q == nil {
			q = &Quant{Name: name}
			by[name] = q
		}
		q.Size += f.Size
		q.Files = append(q.Files, f)
	}
	out := make([]Quant, 0, len(by))
	for _, q := range by {
		out = append(out, *q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size < out[j].Size })
	return out
}

func (r *Repo) MMProj() *File {
	var pick *File
	for i, f := range r.Files {
		if !gguf.IsMMProj(f.Path) || !strings.HasSuffix(f.Path, ".gguf") {
			continue
		}
		if pick == nil || (strings.Contains(strings.ToUpper(f.Path), "F16") && !strings.Contains(strings.ToUpper(pick.Path), "F16")) {
			pick = &r.Files[i]
		}
	}
	return pick
}

func ParseRef(ref string) (repo, quant string, ok bool) {
	ref = strings.TrimSpace(ref)
	for _, p := range []string{"https://", "http://"} {
		ref = strings.TrimPrefix(ref, p)
	}
	for _, p := range []string{"huggingface.co/", "hf.co/"} {
		if rest, found := strings.CutPrefix(ref, p); found {
			ref = rest
			if strings.HasPrefix(ref, "spaces/") || strings.HasPrefix(ref, "datasets/") {
				return "", "", false
			}
			if parts := strings.SplitN(ref, "/", 3); len(parts) >= 2 {
				ref = parts[0] + "/" + parts[1]
			}
		}
	}
	repo, quant, _ = strings.Cut(ref, ":")
	if strings.Count(repo, "/") != 1 || strings.ContainsAny(repo, " \t") || strings.HasPrefix(repo, "/") || strings.HasSuffix(repo, "/") {
		return "", "", false
	}
	return repo, strings.ToUpper(quant), true
}

type Task struct {
	ID    string
	Label string
	Group string
}

type Group struct{ ID, Label string }

var Groups = []Group{
	{"multimodal", "Multimodal"},
	{"cv", "Computer Vision"},
	{"nlp", "Natural Language Processing"},
	{"audio", "Audio"},
	{"tabular", "Tabular"},
	{"rl", "Reinforcement Learning"},
	{"other", "Other"},
}

func (c *Client) Tasks(ctx context.Context) ([]Task, error) {
	var raw struct {
		Pipelines []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Group string `json:"subType"`
		} `json:"pipeline_tag"`
	}
	if err := c.get(ctx, base+"/api/models-tags-by-type", &raw); err != nil {
		return nil, err
	}
	var out []Task
	for _, p := range raw.Pipelines {
		out = append(out, Task{ID: p.ID, Label: p.Label, Group: p.Group})
	}
	return out, nil
}

type Why struct{ Short, Long string }

var notLanguage = map[string]string{
	"text-to-image": "image", "image-to-image": "image", "unconditional-image-generation": "image",
	"text-to-video": "image", "image-to-video": "image", "video-to-video": "image", "text-to-3d": "image", "image-to-3d": "image",
	"text-to-speech": "speech", "automatic-speech-recognition": "speech", "audio-to-audio": "speech",
	"audio-classification": "speech", "voice-activity-detection": "speech", "text-to-audio": "speech",
	"image-classification": "vision", "object-detection": "vision", "image-segmentation": "vision", "depth-estimation": "vision",
}

func notLM(pipeline, arch string, context uint64) *Why {
	switch notLanguage[pipeline] {
	case "image":
		return &Why{"image/video", "Image and video generators run in tools like ComfyUI or stable-diffusion.cpp, not llama.cpp."}
	case "speech":
		return &Why{"speech", "Speech models need their own runtime (whisper.cpp, a TTS server), not llama-server."}
	case "vision":
		return &Why{"not an LLM", "This is a vision model, not a language model: llama.cpp can't run it."}
	}
	if arch != "" && context == 0 {
		return &Why{"not an LLM", "The GGUF has no context length or vocabulary, so it isn't a language model llama.cpp can run."}
	}
	return nil
}

func (s Summary) Runnable() *Why {
	if !hasTag(s.Tags, "gguf") {
		return &Why{"not GGUF", "Not in GGUF format: llama.cpp only runs GGUF. Look for a GGUF conversion of it."}
	}
	return notLM(s.Pipeline, s.GGUF.Arch, s.GGUF.Context)
}

func (r *Repo) Runnable() *Why {
	if len(r.Quants()) == 0 {
		return &Why{"not GGUF", "No GGUF files in this repo: llama.cpp only runs GGUF. Look for a GGUF conversion of it."}
	}
	return notLM(r.Pipeline, r.Arch, r.Context)
}

func hasTag(tags []string, t string) bool {
	for _, x := range tags {
		if x == t {
			return true
		}
	}
	return false
}

func (s Summary) IsGGUF() bool { return hasTag(s.Tags, "gguf") }
