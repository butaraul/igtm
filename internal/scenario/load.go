package scenario

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

//go:embed data/*.yaml
var embedded embed.FS

// All loads and validates every embedded scenario, ordered by difficulty
// and then id. It fails if any scenario is malformed.
func All() ([]*Scenario, error) {
	entries, err := embedded.ReadDir("data")
	if err != nil {
		return nil, err
	}
	var list []*Scenario
	var errs []error
	for _, e := range entries {
		name := path.Join("data", e.Name())
		data, err := embedded.ReadFile(name)
		if err != nil {
			return nil, err
		}
		s, err := Load(e.Name(), data)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		list = append(list, s)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range list {
		if seen[s.ID] {
			return nil, fmt.Errorf("duplicate scenario id %q", s.ID)
		}
		seen[s.ID] = true
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Difficulty != list[j].Difficulty {
			return list[i].Difficulty < list[j].Difficulty
		}
		return list[i].ID < list[j].ID
	})
	return list, nil
}

// Find returns the scenario with the given id, or nil.
func Find(list []*Scenario, id string) *Scenario {
	for _, s := range list {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// Load decodes one scenario file strictly (unknown keys are errors) and
// validates it. name is used in error messages.
func Load(name string, data []byte) (*Scenario, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var s Scenario
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: expected a single YAML document", name)
	}
	p := &Problems{File: name}
	if strings.Contains(strings.ToLower(string(data)), "lorem ipsum") {
		p.add("", "placeholder copy (lorem ipsum)")
	}
	s.validate(p)
	if len(p.List) > 0 {
		return nil, p
	}
	return &s, nil
}

// Problems is every validation failure found in one scenario file.
type Problems struct {
	File string
	List []string
}

func (p *Problems) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d problem", p.File, len(p.List))
	if len(p.List) != 1 {
		b.WriteString("s")
	}
	for _, s := range p.List {
		b.WriteString("\n  ")
		b.WriteString(s)
	}
	return b.String()
}

func (p *Problems) add(at, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if at != "" {
		msg = at + ": " + msg
	}
	p.List = append(p.List, msg)
}
