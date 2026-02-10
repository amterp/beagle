package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/amterp/beagle/internal/core"
	"gopkg.in/yaml.v3"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type Entry struct {
	ConfigPath string `yaml:"config_path"`
	Namespace  string `yaml:"namespace"`
}

type Registry struct {
	Active   string           `yaml:"active"`
	Profiles map[string]Entry `yaml:"profiles"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return core.ProfileRegistryPath(home), nil
}

func Load(path string) (Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Registry{Profiles: map[string]Entry{}}, nil
		}
		return Registry{}, fmt.Errorf("read profiles: %w", err)
	}

	var r Registry
	if err := yaml.Unmarshal(b, &r); err != nil {
		return Registry{}, fmt.Errorf("parse profiles: %w", err)
	}
	if r.Profiles == nil {
		r.Profiles = map[string]Entry{}
	}
	return r, nil
}

func Save(path string, r Registry) error {
	if r.Profiles == nil {
		r.Profiles = map[string]Entry{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create profiles dir: %w", err)
	}
	b, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal profiles: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write profiles: %w", err)
	}
	return nil
}

func ValidateName(name string) error {
	name = strings.TrimSpace(strings.ToLower(name))
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid profile name: %q", name)
	}
	return nil
}

func NormalizeName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

func NamespaceFromName(name string) (string, error) {
	name = NormalizeName(name)
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return name, nil
}

// NamespaceFromPath delegates to core.NamespaceFromPath for hash-based
// namespace derivation from a config file path.
func NamespaceFromPath(path string) string {
	return core.NamespaceFromPath(path)
}

func Register(r *Registry, name string, configPath string) (Entry, error) {
	if r.Profiles == nil {
		r.Profiles = map[string]Entry{}
	}
	name = NormalizeName(name)
	if err := ValidateName(name); err != nil {
		return Entry{}, err
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return Entry{}, fmt.Errorf("resolve config path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return Entry{}, fmt.Errorf("config path invalid: %w", err)
	}
	if _, exists := r.Profiles[name]; exists {
		return Entry{}, fmt.Errorf("profile already exists: %s", name)
	}
	for other, entry := range r.Profiles {
		if entry.ConfigPath == abs {
			return Entry{}, fmt.Errorf("config path already registered by profile %s", other)
		}
	}
	ns, _ := NamespaceFromName(name)

	// Reject namespace collisions across profiles
	for other, entry := range r.Profiles {
		if entry.Namespace == ns && other != name {
			return Entry{}, fmt.Errorf("namespace %q conflicts with profile %s", ns, other)
		}
	}

	entry := Entry{ConfigPath: abs, Namespace: ns}
	r.Profiles[name] = entry
	if r.Active == "" {
		r.Active = name
	}
	return entry, nil
}

func Remove(r *Registry, name string) error {
	if r.Profiles == nil {
		return fmt.Errorf("profile not found: %s", name)
	}
	name = NormalizeName(name)
	if _, ok := r.Profiles[name]; !ok {
		return fmt.Errorf("profile not found: %s", name)
	}
	delete(r.Profiles, name)
	if r.Active == name {
		r.Active = ""
		if len(r.Profiles) > 0 {
			names := make([]string, 0, len(r.Profiles))
			for n := range r.Profiles {
				names = append(names, n)
			}
			sort.Strings(names)
			r.Active = names[0]
		}
	}
	return nil
}

func Use(r *Registry, name string) (Entry, error) {
	if r.Profiles == nil {
		return Entry{}, fmt.Errorf("profile not found: %s", name)
	}
	name = NormalizeName(name)
	entry, ok := r.Profiles[name]
	if !ok {
		return Entry{}, fmt.Errorf("profile not found: %s", name)
	}
	r.Active = name
	return entry, nil
}
