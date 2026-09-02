package conformance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	goBinariesOnce sync.Once
	goBinariesDir  string
	goBinariesErr  error
)

// goBinaries builds the Go driver, probe and fuzzcase once per test run, concurrently, into one temporary directory.
func goBinaries(t *testing.T) string {
	t.Helper()
	goBinariesOnce.Do(func() {
		goBinariesDir, goBinariesErr = os.MkdirTemp("", "gofake")
		if goBinariesErr != nil {
			return
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, name := range []string{"godriver", "proberef", "fuzzcase"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cmd := exec.Command("go", "build", "-o", filepath.Join(goBinariesDir, name), "./internal/conformance/"+name)
				cmd.Dir = "../.."
				if out, err := cmd.CombinedOutput(); err != nil {
					mu.Lock()
					goBinariesErr = err
					mu.Unlock()
					t.Logf("build %s: %v\n%s", name, err, out)
				}
			}()
		}
		wg.Wait()
	})
	if goBinariesErr != nil {
		t.Fatal(goBinariesErr)
	}
	return goBinariesDir
}

func TestMain(m *testing.M) {
	code := m.Run()
	if goBinariesDir != "" {
		_ = os.RemoveAll(goBinariesDir)
	}
	os.Exit(code)
}

// goBackend describes the Go binaries as a backend, so the kit checks the Go engine as if it were a target.
// That exercises every step with a backend that must pass.
func goBackend(t *testing.T) *Backend {
	t.Helper()
	bin := goBinaries(t)
	dir := t.TempDir()
	manifest := `{
  "name": "gofake",
  "generated": [],
  "engine_symbols": "^github.com/oneregex/revera/go\\.",
  "release": {
    "build": [["true"]],
    "driver": "godriver",
    "probe": "proberef",
    "fuzzcase": "fuzzcase"
  },
  "checked": [
    {"name": "again", "build": [["true"]], "driver": "godriver", "probe": "proberef", "fuzzcase": "fuzzcase"},
    {"name": "absent", "requires": ["no-such-tool-anywhere", "--version"], "build": [["true"]], "driver": "godriver", "probe": "proberef"}
  ]
}
`
	for _, name := range []string{"godriver", "proberef", "fuzzcase"} {
		if err := os.Symlink(filepath.Join(bin, name), filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "backend.json")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := LoadBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestKitPassesOnTheGoEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds three binaries and replays the quick corpus")
	}
	b := goBackend(t)
	skip, err := ParseSkips("generated,lean-data")
	if err != nil {
		t.Fatal(err)
	}
	summary := Run([]*Backend{b}, Options{
		Repo:   repoRoot(t),
		Stress: 1,
		Seed:   DefaultExtraSeed,
		Quick:  true,
		Skip:   skip,
	})
	if summary.Failed() {
		summary.WriteTable(os.Stderr)
		t.Fatal("the Go engine must pass its own kit")
	}
	steps := map[string]Status{}
	for _, r := range summary {
		steps[r.Backend+"/"+r.Step] = r.Status
	}
	for _, step := range []string{"repo/corpus", "gofake/build", "gofake/probe", "gofake/corpus", "gofake/stress", "gofake/fuzz",
		"gofake/checked/again/build", "gofake/checked/again/probe", "gofake/checked/again/corpus", "gofake/checked/again/fuzz"} {
		if steps[step] != Passed {
			t.Fatalf("%s: %s", step, steps[step])
		}
	}
	if steps["gofake/checked/absent/build"] != Skipped {
		t.Fatalf("a checked build with a missing tool must be skipped, got %s", steps["gofake/checked/absent/build"])
	}
	if summary.Skips() != 1 {
		t.Fatalf("expected one skip, got %d", summary.Skips())
	}
	if v := summary.Verdict("gofake"); !strings.Contains(v, "skipped checked/absent/build") {
		t.Fatalf("verdict %q must name the skipped step", v)
	}
}

func TestKitReportsFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries")
	}
	b := goBackend(t)
	wrong := filepath.Join(b.Dir, "wrong.sh")
	if err := os.WriteFile(wrong, []byte("#!/bin/sh\ncat >/dev/null\necho 'P 0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Release.Driver = "wrong.sh"
	b.Release.Fuzzcase = "wrong.sh"
	b.Checked = nil
	skip, _ := ParseSkips("generated,lean-data,stress")
	summary := Run([]*Backend{b}, Options{Repo: repoRoot(t), Quick: true, Skip: skip})
	if !summary.Failed() {
		t.Fatal("a wrong driver must fail")
	}
	var corpus, fuzz, probe Result
	for _, r := range summary {
		switch r.Step {
		case "corpus":
			corpus = r
		case "fuzz":
			fuzz = r
		case "probe":
			probe = r
		}
	}
	if corpus.Status != Failed || !strings.Contains(corpus.Detail, "mismatched") {
		t.Fatalf("corpus step: %+v", corpus)
	}
	if fuzz.Status != Failed || !strings.Contains(fuzz.Detail, "did not report") {
		t.Fatalf("fuzz step: %+v", fuzz)
	}
	if probe.Status != Passed {
		t.Fatalf("probe step: %+v", probe)
	}
	if v := summary.Verdict("gofake"); !strings.HasPrefix(v, "gofake: NOT conformant (corpus, fuzz)") {
		t.Fatalf("verdict %q", v)
	}
}

func TestLoadBackendRejectsBadManifests(t *testing.T) {
	dir := t.TempDir()
	write := func(text string) string {
		path := filepath.Join(dir, "backend.json")
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, bad := range []string{
		`{}`,
		`{"name": "repo", "release": {"build": [["true"]], "driver": "d", "probe": "p"}}`,
		`{"name": "x", "release": {"build": [], "driver": "d", "probe": "p"}}`,
		`{"name": "x", "release": {"build": [["true"]], "driver": "/abs/d", "probe": "p"}}`,
		`{"name": "x", "generated": ["../x"], "release": {"build": [["true"]], "driver": "d", "probe": "p"}}`,
		`{"name": "x", "engine_symbols": "(", "release": {"build": [["true"]], "driver": "d", "probe": "p"}}`,
		`{"name": "x", "release": {"build": [["true"]], "driver": "d", "probe": "p"}, "checked": [{"build": [["true"]], "driver": "d", "probe": "p"}]}`,
		`{"name": "x", "release": {"build": [["true"]], "driver": "d", "probe": "p"}, "checked": [{"name": "a", "build": [["true"]], "driver": "d", "probe": "p"}, {"name": "a", "build": [["true"]], "driver": "d", "probe": "p"}]}`,
	} {
		if _, err := LoadBackend(write(bad)); err == nil {
			t.Fatalf("manifest accepted: %s", bad)
		}
	}
	b, err := LoadBackend(write(`{"name": "x", "engine_symbols": "^e", "release": {"build": [["true"]], "driver": "bin/d", "probe": "p"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.Path("bin/d") != filepath.Join(dir, "bin", "d") || b.Symbols == nil || !b.Symbols.MatchString("engine") {
		t.Fatalf("manifest loaded as %+v", b)
	}
}

func TestParseSkips(t *testing.T) {
	skip, err := ParseSkips(" build, lean ")
	if err != nil || !skip["build"] || !skip["lean"] || len(skip) != 2 {
		t.Fatalf("ParseSkips gave %v, %v", skip, err)
	}
	if _, err := ParseSkips("build,nothing"); err == nil {
		t.Fatal("an unknown step must be rejected")
	}
}
