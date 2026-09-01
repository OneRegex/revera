package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Build names one way to build a backend and the binaries it produces.
// Paths are relative to the backend directory.
type Build struct {
	// Name identifies the build in reports; the release build has no name.
	Name string `json:"name,omitempty"`
	// Requires is a command that must succeed before the build runs.
	// A failing command marks the build as skipped, for toolchains that are optional.
	Requires []string `json:"requires,omitempty"`
	// Commands are the argument vectors that build the binaries, run in order in the backend directory.
	Commands [][]string `json:"build"`
	Driver   string     `json:"driver"`
	Probe    string     `json:"probe"`
	Bench    string     `json:"bench,omitempty"`
	Fuzzcase string     `json:"fuzzcase,omitempty"`
}

// binaries lists the paths the build must produce.
func (bd Build) binaries() []string {
	var out []string
	for _, rel := range []string{bd.Driver, bd.Probe, bd.Bench, bd.Fuzzcase} {
		if rel != "" {
			out = append(out, rel)
		}
	}
	return out
}

// Backend is the manifest of one target, read from backend.json in its directory.
type Backend struct {
	Name string `json:"name"`
	// Generated lists the generated sources, for the code-size report.
	Generated []string `json:"generated"`
	// EngineSymbols is a regular expression over symbol names.
	// The code-size report sums the machine code of the symbols it matches in the release driver.
	EngineSymbols string `json:"engine_symbols"`
	// Toolchain is a command that prints the compiler version, for report headers.
	Toolchain []string `json:"toolchain,omitempty"`
	// Release is the optimized build that the corpus, stress, bench and fuzz steps use.
	Release Build `json:"release"`
	// Checked lists the builds with runtime checks, such as sanitizers or debug modes.
	// Each one runs the probe, the light corpus and the fuzz pack.
	Checked []Build `json:"checked"`
	// Dir is the backend directory, set by the loader.
	Dir string `json:"-"`
	// Symbols is EngineSymbols compiled, or nil when the manifest has none.
	Symbols *regexp.Regexp `json:"-"`
}

// LoadBackend reads and validates one manifest.
func LoadBackend(path string) (*Backend, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b := &Backend{}
	if err := json.Unmarshal(data, b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	b.Dir = abs
	if err := b.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}

func (b *Backend) validate() error {
	if b.Name == "" || b.Name == repoScope {
		return fmt.Errorf("manifest needs a name other than %q", repoScope)
	}
	for _, rel := range b.Generated {
		if err := localPath(rel); err != nil {
			return fmt.Errorf("generated: %w", err)
		}
	}
	if b.EngineSymbols != "" {
		re, err := regexp.Compile(b.EngineSymbols)
		if err != nil {
			return fmt.Errorf("engine_symbols: %w", err)
		}
		b.Symbols = re
	}
	if err := b.Release.validate("release"); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, c := range b.Checked {
		if c.Name == "" {
			return fmt.Errorf("a checked build has no name")
		}
		if seen[c.Name] {
			return fmt.Errorf("checked build %q appears twice", c.Name)
		}
		seen[c.Name] = true
		if err := c.validate("checked build " + c.Name); err != nil {
			return err
		}
	}
	return nil
}

func (bd *Build) validate(what string) error {
	if len(bd.Commands) == 0 {
		return fmt.Errorf("%s has no build command", what)
	}
	for _, argv := range bd.Commands {
		if len(argv) == 0 {
			return fmt.Errorf("%s has an empty build command", what)
		}
	}
	if bd.Driver == "" || bd.Probe == "" {
		return fmt.Errorf("%s needs a driver and a probe", what)
	}
	for _, rel := range bd.binaries() {
		if err := localPath(rel); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
	}
	return nil
}

func localPath(rel string) error {
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("path %q is not relative to the backend directory", rel)
	}
	return nil
}

// Path resolves a manifest-relative path.
func (b *Backend) Path(rel string) string {
	return filepath.Join(b.Dir, filepath.FromSlash(rel))
}

// DiscoverBackends loads every backend.json one level below the repository root, sorted by name.
func DiscoverBackends(repo string) ([]*Backend, error) {
	matches, err := filepath.Glob(filepath.Join(repo, "*", "backend.json"))
	if err != nil {
		return nil, err
	}
	var backends []*Backend
	for _, path := range matches {
		b, err := LoadBackend(path)
		if err != nil {
			return nil, err
		}
		backends = append(backends, b)
	}
	sort.Slice(backends, func(i, j int) bool { return backends[i].Name < backends[j].Name })
	return backends, nil
}

// SelectBackends loads the manifests named by a command line, relative to cwd, or discovers all of them when there are none.
// A value may name the backend directory or the manifest file.
func SelectBackends(repo, cwd string, values []string) ([]*Backend, error) {
	if len(values) == 0 {
		backends, err := DiscoverBackends(repo)
		if err != nil {
			return nil, err
		}
		if len(backends) == 0 {
			return nil, fmt.Errorf("no backend.json found below %s", repo)
		}
		return backends, nil
	}
	var backends []*Backend
	// A backend named twice would build twice in one directory at once.
	seen := map[string]bool{}
	for _, value := range values {
		path := value
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			path = filepath.Join(path, "backend.json")
		}
		b, err := LoadBackend(path)
		if err != nil {
			return nil, err
		}
		if seen[b.Dir] {
			continue
		}
		seen[b.Dir] = true
		backends = append(backends, b)
	}
	return backends, nil
}

// BuildBinaries runs the build commands of one build and checks that every named binary exists afterwards.
// It returns the combined output of the commands.
func BuildBinaries(b *Backend, bd Build) (string, error) {
	var all strings.Builder
	for _, argv := range bd.Commands {
		out, err := command(b.Dir, argv[0], argv[1:]...)
		all.WriteString(out)
		if err != nil {
			return all.String(), err
		}
	}
	for _, rel := range bd.binaries() {
		if _, err := os.Stat(b.Path(rel)); err != nil {
			return all.String(), fmt.Errorf("the build did not produce %s", rel)
		}
	}
	return all.String(), nil
}

// command runs one program in dir and returns its combined output.
// A build or a Lean replay can take as long as it needs, so nothing bounds it.
func command(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}

// boundedCommand is command under ProtocolTimeout, for a binary that must answer.
func boundedCommand(dir string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ProtocolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return string(out), fmt.Errorf("%s %s: no result after %s", name, strings.Join(args, " "), ProtocolTimeout)
		}
		return string(out), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}
