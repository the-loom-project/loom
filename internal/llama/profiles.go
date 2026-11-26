package llama

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Last is what a model was last run with, and the profile it started from.
type Last struct {
	Profile string `json:"profile,omitempty"`
	Config  Config `json:"config"`
}

type profileFile struct {
	Last     map[string]Last              `json:"last"`
	Profiles map[string]map[string]Config `json:"profiles"`
}

type Profiles struct {
	path string
	f    profileFile
}

type Entry struct{ Ref, Name string }

func LoadProfiles() *Profiles {
	dir, _ := os.UserConfigDir()
	p := &Profiles{path: filepath.Join(dir, "loom", "profiles.json")}
	if b, err := os.ReadFile(p.path); err == nil {
		json.Unmarshal(b, &p.f)
	}
	if p.f.Last == nil {
		p.f.Last = map[string]Last{}
	}
	if p.f.Profiles == nil {
		p.f.Profiles = map[string]map[string]Config{}
	}
	return p
}

func (p *Profiles) save() {
	if b, err := json.MarshalIndent(p.f, "", "  "); err == nil {
		os.MkdirAll(filepath.Dir(p.path), 0o755)
		os.WriteFile(p.path, b, 0o644)
	}
}

// Get is the config a model starts from: what it was last run with, else the defaults.
func (p *Profiles) Get(e Env) Config {
	if l, ok := p.f.Last[e.Model.Ref]; ok {
		return l.Config.Fix(e)
	}
	return Defaults(e).Fix(e)
}

// LastProfile is the profile the model's last run started from, or "".
func (p *Profiles) LastProfile(ref string) string {
	name := p.f.Last[ref].Profile
	if _, ok := p.Profile(ref, name); !ok {
		return ""
	}
	return name
}

func (p *Profiles) Remember(ref, profile string, c Config) {
	p.f.Last[ref] = Last{Profile: profile, Config: c}
	p.save()
}

func (p *Profiles) Profile(ref, name string) (Config, bool) {
	c, ok := p.f.Profiles[ref][name]
	return c, ok
}

func (p *Profiles) Names(ref string) []string {
	var names []string
	for name := range p.f.Profiles[ref] {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return names
}

func (p *Profiles) All() []Entry {
	var out []Entry
	for ref := range p.f.Profiles {
		for _, name := range p.Names(ref) {
			out = append(out, Entry{ref, name})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Ref) < strings.ToLower(out[j].Ref)
	})
	return out
}

func (p *Profiles) Save(ref, name string, c Config) {
	if p.f.Profiles[ref] == nil {
		p.f.Profiles[ref] = map[string]Config{}
	}
	p.f.Profiles[ref][name] = c
	p.save()
}

func (p *Profiles) Delete(ref, name string) {
	delete(p.f.Profiles[ref], name)
	if len(p.f.Profiles[ref]) == 0 {
		delete(p.f.Profiles, ref)
	}
	p.save()
}

func (p *Profiles) Rename(ref, from, to string) error {
	if from == to {
		return nil
	}
	if _, taken := p.Profile(ref, to); taken {
		return fmt.Errorf("%q already exists", to)
	}
	c, _ := p.Profile(ref, from)
	delete(p.f.Profiles[ref], from)
	p.f.Profiles[ref][to] = c
	if l, ok := p.f.Last[ref]; ok && l.Profile == from {
		l.Profile = to
		p.f.Last[ref] = l
	}
	p.save()
	return nil
}
