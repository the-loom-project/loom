package llama

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"loom/internal/jsonfile"
)

// Last is what a model was last run with, and the profile it started from.
type Last struct {
	Profile string `json:"profile,omitempty"`
	Config  Config `json:"config"`
}

type Failure struct {
	Build  string `json:"build"`
	Reason string `json:"reason"`
}

type profileFile struct {
	Last     map[string]Last              `json:"last"`
	Profiles map[string]map[string]Config `json:"profiles"`
	Failed   map[string]Failure           `json:"failed,omitempty"`
}

type Profiles struct {
	path string
	f    profileFile
}

type Entry struct{ Ref, Name string }

func LoadProfiles() (*Profiles, error) {
	dir, _ := os.UserConfigDir()
	p := &Profiles{path: filepath.Join(dir, "loom", "profiles.json")}
	err := jsonfile.Read(p.path, &p.f)
	if err != nil {
		p.f = profileFile{}
	}
	if p.f.Last == nil {
		p.f.Last = map[string]Last{}
	}
	if p.f.Profiles == nil {
		p.f.Profiles = map[string]map[string]Config{}
	}
	return p, err
}

func (p *Profiles) save() error { return jsonfile.Write(p.path, p.f) }

// Get is the config a model starts from: what it was last run with, else the defaults.
func (p *Profiles) Get(e Env) Config {
	if l, ok := p.f.Last[e.Model.Ref]; ok {
		return l.Config.Fix(e)
	}
	return Defaults().Fix(e)
}

// LastProfile is the profile the model's last run started from, or "".
func (p *Profiles) LastProfile(ref string) string {
	name := p.f.Last[ref].Profile
	if _, ok := p.Profile(ref, name); !ok {
		return ""
	}
	return name
}

func (p *Profiles) Remember(ref, profile string, c Config) error {
	p.f.Last[ref] = Last{Profile: profile, Config: c}
	return p.save()
}

func (p *Profiles) Failure(ref, build string) string {
	if f, ok := p.f.Failed[ref]; ok && f.Build == build {
		return f.Reason
	}
	return ""
}

func (p *Profiles) SetFailure(ref, build, reason string) error {
	switch _, known := p.f.Failed[ref]; {
	case reason != "":
		if p.f.Failed == nil {
			p.f.Failed = map[string]Failure{}
		}
		p.f.Failed[ref] = Failure{build, reason}
	case known:
		delete(p.f.Failed, ref)
	default:
		return nil
	}
	return p.save()
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

func (p *Profiles) Save(ref, name string, c Config) error {
	if p.f.Profiles[ref] == nil {
		p.f.Profiles[ref] = map[string]Config{}
	}
	p.f.Profiles[ref][name] = c
	return p.save()
}

func (p *Profiles) Delete(ref, name string) error {
	delete(p.f.Profiles[ref], name)
	if len(p.f.Profiles[ref]) == 0 {
		delete(p.f.Profiles, ref)
	}
	return p.save()
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
	return p.save()
}
