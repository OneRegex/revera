package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/oneregex/revera/dev/internal/conformance"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func testTempDir(t *testing.T) string {
	t.Helper()
	repo, err := conformance.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repo, "tmp")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "revera-cli-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove test directory: %v", err)
		}
	})
	return dir
}

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func createFixtureRepository(t *testing.T, parent string) string {
	t.Helper()
	repo := filepath.Join(parent, "repository with spaces")
	for _, rel := range []string{"go", "vego/probe", "dev", "rust", "zig", "ts", "native/c", "native/cpp", "tmp"} {
		if err := os.MkdirAll(filepath.Join(repo, filepath.FromSlash(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(repo, "go.work"), []byte("go 1.27.0\n\nuse (\n\t./dev\n\t./go\n\t./vego\n)\n"))
	writeTestFile(t, filepath.Join(repo, "go", "go.mod"), []byte("module github.com/oneregex/revera/go\n\ngo 1.27.0\n"))
	return repo
}

func generatedContent(rel string) []byte {
	return []byte("generated:" + rel + "\n")
}

func seedArtifacts(t *testing.T, repo string, artifacts []artifact, generated bool) map[string][]byte {
	t.Helper()
	contents := map[string][]byte{}
	for _, artifact := range artifacts {
		content := []byte("original:" + artifact.rel + "\n")
		if generated {
			content = generatedContent(artifact.rel)
		}
		contents[artifact.rel] = slices.Clone(content)
		writeTestFile(t, filepath.Join(repo, filepath.FromSlash(artifact.rel)), content)
	}
	return contents
}

type fakeRunner struct {
	calls  []generationStep
	failAt int
	omit   map[string]bool
	link   map[string]string
}

type faultingInstallationFiles struct {
	operatingSystemFiles
	renameCalls int
	failRename  map[int]bool
}

func (f *faultingInstallationFiles) Rename(oldPath, newPath string) error {
	f.renameCalls++
	if f.failRename[f.renameCalls] {
		return fmt.Errorf("synthetic rename failure %d", f.renameCalls)
	}
	return f.operatingSystemFiles.Rename(oldPath, newPath)
}

func (r *fakeRunner) Run(dir string, step generationStep, stderr io.Writer) error {
	r.calls = append(r.calls, step)
	idx := len(r.calls) - 1
	if r.failAt >= 0 && idx == r.failAt {
		fmt.Fprintln(stderr, "synthetic child diagnostic")
		return errors.New("synthetic generator failure")
	}
	for _, output := range step.outputs {
		if r.omit[output.artifact.rel] {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(output.path), 0o755); err != nil {
			return err
		}
		if target := r.link[output.artifact.rel]; target != "" {
			if err := os.Symlink(target, output.path); err != nil {
				return err
			}
			continue
		}
		if err := os.WriteFile(output.path, generatedContent(output.artifact.rel), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func successfulFakeRunner() *fakeRunner {
	return &fakeRunner{failAt: -1, omit: map[string]bool{}, link: map[string]string{}}
}

func generationTempEntries(t *testing.T, repo string) []string {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(repo, "tmp", "revera-generate-*"))
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestParseTargets(t *testing.T) {
	all := targetSet{"rust": true, "zig": true, "ts": true, "cpp": true, "c": true}
	cases := []struct {
		name   string
		values []string
		want   targetSet
	}{
		{name: "default", want: all},
		{name: "all", values: []string{"all"}, want: all},
		{name: "comma list", values: []string{"rust, zig,ts,cpp,c"}, want: all},
		{name: "repeated", values: []string{"cpp", "rust", "ts", "zig", "c"}, want: all},
		{name: "deduplicated", values: []string{"rust,rust"}, want: targetSet{"rust": true}},
		{name: "one", values: []string{" zig "}, want: targetSet{"zig": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTargets(tc.values)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("targets = %#v, want %#v", got, tc.want)
			}
		})
	}

	for _, values := range [][]string{
		{""}, {"rust,"}, {"unknown"}, {"Rust"}, {"all,rust"}, {"all", "all"},
	} {
		if _, err := parseTargets(values); err == nil {
			t.Errorf("parseTargets(%q) succeeded", values)
		}
	}
}

func TestPreserveStageErrorIsIdempotent(t *testing.T) {
	original := errors.New("rollback failed")
	preserved := preserveStage(original)
	if preserveStage(preserved) != preserved {
		t.Fatal("preserveStage wrapped an already preserved error")
	}
	var marker *preserveStageError
	if !errors.As(preserved, &marker) || !errors.Is(preserved, original) {
		t.Fatalf("preserved error lost its cause: %v", preserved)
	}
}

func TestArtifactsAndPlanAreDeterministic(t *testing.T) {
	stage := filepath.Join(testTempDir(t), "stage with spaces")
	targets, err := parseTargets(nil)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := artifactsFor(targets)
	wantArtifacts := []string{
		"revera.vego.json", "vego/probe/probe.vego.json",
		"rust/src/engine.rs", "rust/src/probe_engine.rs",
		"zig/src/engine.zig", "zig/src/probe_engine.zig",
		"ts/src/engine.ts", "ts/src/probe_engine.ts",
		"native/cpp/engine.hpp", "native/cpp/engine.cpp",
		"native/cpp/probe_engine.hpp", "native/cpp/probe_engine.cpp",
		"native/c/engine.h", "native/c/engine.c",
		"native/c/probe_engine.h", "native/c/probe_engine.c",
	}
	var gotArtifacts []string
	for _, artifact := range artifacts {
		gotArtifacts = append(gotArtifacts, artifact.rel)
	}
	if !slices.Equal(gotArtifacts, wantArtifacts) {
		t.Fatalf("artifacts = %q, want %q", gotArtifacts, wantArtifacts)
	}

	plan := generationPlan(filepath.Dir(stage), stage, targets)
	wantSteps := []string{
		"export revera IR", "export probe IR",
		"generate Rust engine", "generate Rust probe",
		"generate Zig engine", "generate Zig probe",
		"generate TypeScript engine", "generate TypeScript probe",
		"generate C++ engine", "generate C++ probe",
		"generate C engine", "generate C probe",
	}
	var gotSteps []string
	for _, step := range plan {
		gotSteps = append(gotSteps, step.name)
	}
	if !slices.Equal(gotSteps, wantSteps) {
		t.Fatalf("steps = %q, want %q", gotSteps, wantSteps)
	}
	cpp := plan[8]
	if !slices.Contains(cpp.args, "revera::engine") {
		t.Fatalf("C++ engine arguments omit the namespace: %q", cpp.args)
	}
	c := plan[10]
	if !slices.Contains(c.args, "revera_eng") {
		t.Fatalf("C engine arguments omit the prefix: %q", c.args)
	}
	for _, step := range plan {
		for _, output := range step.outputs {
			if !strings.HasPrefix(output.path, stage+string(filepath.Separator)) {
				t.Errorf("output escaped the stage: %s", output.path)
			}
		}
	}
}

func TestFindRepositoryRoot(t *testing.T) {
	parent := testTempDir(t)
	repo := createFixtureRepository(t, parent)
	deep := filepath.Join(repo, "dev", "cmd", "nested")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(repo, "dev", "cmd")
	writeTestFile(t, filepath.Join(decoy, "go.work"), []byte("go 1.27.0\n"))
	writeTestFile(t, filepath.Join(decoy, "go", "go.mod"), []byte("module github.com/oneregex/revera/go\n"))

	for _, start := range []string{repo, filepath.Join(repo, "dev"), deep} {
		got, err := conformance.FindRepositoryRoot(start)
		if err != nil {
			t.Fatal(err)
		}
		if got != repo {
			t.Fatalf("root from %s = %s, want %s", start, got, repo)
		}
	}
	writeTestFile(t, filepath.Join(repo, "go", "go.mod"), []byte("module github.com/oneregex/revera/go\r\n\r\ngo 1.27.0\r\n"))
	if got, err := conformance.FindRepositoryRoot(deep); err != nil || got != repo {
		t.Fatalf("root from CRLF go.mod = %s, %v; want %s", got, err, repo)
	}

	link := filepath.Join(parent, "repository-link")
	if err := os.Symlink(repo, link); err == nil {
		got, err := conformance.FindRepositoryRoot(filepath.Join(link, "dev"))
		if err != nil {
			t.Fatal(err)
		}
		if got != repo {
			t.Fatalf("root through symlink = %s, want %s", got, repo)
		}
	}

	actualRepo, err := conformance.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Dir(actualRepo)
	if _, err := conformance.FindRepositoryRoot(outside); err == nil {
		t.Fatal("root discovery succeeded outside a repository")
	}
}

func TestGenerateSelectedTarget(t *testing.T) {
	repo := createFixtureRepository(t, testTempDir(t))
	all, _ := parseTargets(nil)
	original := seedArtifacts(t, repo, artifactsFor(all), false)
	targets := targetSet{"rust": true}
	runner := successfulFakeRunner()
	result, err := executeWorkflow(repo, targets, false, runner, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	wantUpdated := []string{
		"revera.vego.json", "vego/probe/probe.vego.json",
		"rust/src/engine.rs", "rust/src/probe_engine.rs",
	}
	if !slices.Equal(result.updated, wantUpdated) {
		t.Fatalf("updated = %q, want %q", result.updated, wantUpdated)
	}
	selected := map[string]bool{}
	for _, rel := range wantUpdated {
		selected[rel] = true
	}
	for _, artifact := range artifactsFor(all) {
		got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(artifact.rel)))
		if err != nil {
			t.Fatal(err)
		}
		want := original[artifact.rel]
		if selected[artifact.rel] {
			want = generatedContent(artifact.rel)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s = %q, want %q", artifact.rel, got, want)
		}
	}
	if entries := generationTempEntries(t, repo); len(entries) != 0 {
		t.Fatalf("generation left temporary entries: %q", entries)
	}
}

func TestGenerationFailureIsTransactional(t *testing.T) {
	targets, _ := parseTargets(nil)
	steps := generationPlan("repo", "stage", targets)
	for failAt := range steps {
		t.Run(steps[failAt].name, func(t *testing.T) {
			repo := createFixtureRepository(t, testTempDir(t))
			artifacts := artifactsFor(targets)
			original := seedArtifacts(t, repo, artifacts, false)
			runner := successfulFakeRunner()
			runner.failAt = failAt
			var stderr bytes.Buffer
			if _, err := executeWorkflow(repo, targets, false, runner, &stderr); err == nil {
				t.Fatal("generation succeeded")
			}
			if !strings.Contains(stderr.String(), "synthetic child diagnostic") {
				t.Fatalf("child stderr was lost: %q", stderr.String())
			}
			assertArtifactContents(t, repo, original)
			if entries := generationTempEntries(t, repo); len(entries) != 0 {
				t.Fatalf("generation left temporary entries: %q", entries)
			}
		})
	}
}

func TestMissingGeneratorOutputIsTransactional(t *testing.T) {
	repo := createFixtureRepository(t, testTempDir(t))
	targets := targetSet{"cpp": true}
	artifacts := artifactsFor(targets)
	original := seedArtifacts(t, repo, artifacts, false)
	runner := successfulFakeRunner()
	runner.omit["native/cpp/engine.cpp"] = true
	_, err := executeWorkflow(repo, targets, false, runner, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "did not create native/cpp/engine.cpp") {
		t.Fatalf("generation error = %v", err)
	}
	assertArtifactContents(t, repo, original)
}

func TestSymlinkedGeneratorOutputIsTransactional(t *testing.T) {
	repo := createFixtureRepository(t, testTempDir(t))
	targets := targetSet{"rust": true}
	artifacts := artifactsFor(targets)
	original := seedArtifacts(t, repo, artifacts, false)
	outside := filepath.Join(testTempDir(t), "generated-sentinel")
	writeTestFile(t, outside, []byte("outside\n"))
	runner := successfulFakeRunner()
	runner.link[artifacts[0].rel] = outside
	_, err := executeWorkflow(repo, targets, false, runner, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "non-regular output") {
		t.Fatalf("generation error = %v", err)
	}
	assertArtifactContents(t, repo, original)
	content, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "outside\n" {
		t.Fatalf("generation changed symlink target: %q", content)
	}
}

func TestInstallationPreflightAvoidsPartialWrites(t *testing.T) {
	repo := createFixtureRepository(t, testTempDir(t))
	targets := targetSet{"rust": true}
	artifacts := artifactsFor(targets)
	original := seedArtifacts(t, repo, artifacts, false)
	bad := "rust/src/probe_engine.rs"
	if err := os.Remove(filepath.Join(repo, filepath.FromSlash(bad))); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, filepath.FromSlash(bad)), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := executeWorkflow(repo, targets, false, successfulFakeRunner(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("generation error = %v", err)
	}
	delete(original, bad)
	assertArtifactContents(t, repo, original)
}

func TestGenerationReplacesDestinationSymlink(t *testing.T) {
	repo := createFixtureRepository(t, testTempDir(t))
	targets := targetSet{"rust": true}
	artifacts := artifactsFor(targets)
	seedArtifacts(t, repo, artifacts, false)
	destination := filepath.Join(repo, "rust", "src", "engine.rs")
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(testTempDir(t), "outside-sentinel")
	writeTestFile(t, outside, []byte("outside\n"))
	if err := os.Symlink(outside, destination); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := executeWorkflow(repo, targets, false, successfulFakeRunner(), io.Discard); err != nil {
		t.Fatal(err)
	}
	outsideContent, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideContent) != "outside\n" {
		t.Fatalf("generation followed the destination symlink: %q", outsideContent)
	}
	info, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("destination is still a symlink")
	}
}

func TestWorkflowRejectsSymlinkedArtifactParents(t *testing.T) {
	for _, level := range []string{"top-level", "nested"} {
		for _, check := range []bool{false, true} {
			name := fmt.Sprintf("%s/check=%t", level, check)
			t.Run(name, func(t *testing.T) {
				repo := createFixtureRepository(t, testTempDir(t))
				targets := targetSet{"rust": true}
				seedArtifacts(t, repo, artifactsFor(targets), false)
				outsideRoot := filepath.Join(testTempDir(t), "outside")
				outsideParent := filepath.Join(outsideRoot, "src")
				link := filepath.Join(repo, "rust", "src")
				if level == "top-level" {
					outsideParent = outsideRoot
					link = filepath.Join(repo, "rust")
				}
				outsideArtifact := filepath.Join(outsideParent, "engine.rs")
				writeTestFile(t, outsideArtifact, []byte("outside\n"))
				if err := os.RemoveAll(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outsideParent, link); err != nil {
					t.Skipf("symlinks are unavailable: %v", err)
				}
				runner := successfulFakeRunner()
				if _, err := executeWorkflow(repo, targets, check, runner, io.Discard); err == nil ||
					!strings.Contains(err.Error(), "contains a symlink") {
					t.Fatalf("workflow error = %v", err)
				}
				if len(runner.calls) != 0 {
					t.Fatalf("generator ran before path validation: %#v", runner.calls)
				}
				content, err := os.ReadFile(outsideArtifact)
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != "outside\n" {
					t.Fatalf("outside artifact changed: %q", content)
				}
			})
		}
	}
}

func TestWorkflowRejectsSymlinkedTemporaryDirectory(t *testing.T) {
	repo := createFixtureRepository(t, testTempDir(t))
	targets := targetSet{"rust": true}
	seedArtifacts(t, repo, artifactsFor(targets), false)
	tmpPath := filepath.Join(repo, "tmp")
	if err := os.Remove(tmpPath); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(testTempDir(t), "outside-tmp")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, tmpPath); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	runner := successfulFakeRunner()
	if _, err := executeWorkflow(repo, targets, false, runner, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "contains a symlink") {
		t.Fatalf("workflow error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("generator ran before temporary path validation: %#v", runner.calls)
	}
}

func TestInstallationRenameFailuresAreRecoverable(t *testing.T) {
	cases := []struct {
		name          string
		failRename    map[int]bool
		preserveStage bool
	}{
		{name: "backup", failRename: map[int]bool{1: true}},
		{name: "install", failRename: map[int]bool{2: true}},
		{name: "immediate restore", failRename: map[int]bool{2: true, 3: true}, preserveStage: true},
		{name: "rollback restore", failRename: map[int]bool{4: true, 6: true}, preserveStage: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := createFixtureRepository(t, testTempDir(t))
			targets := targetSet{}
			artifacts := artifactsFor(targets)
			original := seedArtifacts(t, repo, artifacts, false)
			files := &faultingInstallationFiles{failRename: tc.failRename}
			_, err := executeWorkflowWithFiles(repo, targets, false, successfulFakeRunner(),
				io.Discard, files)
			if err == nil || !strings.Contains(err.Error(), "synthetic rename failure") {
				t.Fatalf("workflow error = %v", err)
			}
			stages := generationTempEntries(t, repo)
			if tc.preserveStage {
				if len(stages) != 1 || !strings.Contains(err.Error(), "recovery data retained") {
					t.Fatalf("recovery stage = %q, error = %v", stages, err)
				}
				first := artifacts[0].rel
				backup, readErr := os.ReadFile(filepath.Join(stages[0], "backups", filepath.FromSlash(first)))
				if readErr != nil {
					t.Fatal(readErr)
				}
				if !bytes.Equal(backup, original[first]) {
					t.Fatalf("backup = %q, want %q", backup, original[first])
				}
				return
			}
			if len(stages) != 0 {
				t.Fatalf("successful restoration retained stages: %q", stages)
			}
			assertArtifactContents(t, repo, original)
		})
	}
}

func TestCheckGeneratedDetectsContentProblems(t *testing.T) {
	repo := createFixtureRepository(t, testTempDir(t))
	targets, _ := parseTargets(nil)
	artifacts := artifactsFor(targets)
	seedArtifacts(t, repo, artifacts, true)
	cleanPath := filepath.Join(repo, filepath.FromSlash(artifacts[0].rel))
	old := time.Unix(1, 0)
	if err := os.Chtimes(cleanPath, old, old); err != nil {
		t.Fatal(err)
	}
	result, err := executeWorkflow(repo, targets, true, successfulFakeRunner(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.problems) != 0 {
		t.Fatalf("clean check reported: %#v", result.problems)
	}

	writeTestFile(t, filepath.Join(repo, "native", "cpp", "engine.cpp"), []byte("stale\r\n"))
	if err := os.Remove(filepath.Join(repo, "rust", "src", "engine.rs")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repo, "zig", "src", "engine.zig"), nil)
	result, err = executeWorkflow(repo, targets, true, successfulFakeRunner(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := []artifactProblem{
		{kind: "stale", rel: "native/cpp/engine.cpp"},
		{kind: "missing", rel: "rust/src/engine.rs"},
		{kind: "stale", rel: "zig/src/engine.zig"},
	}
	if !reflect.DeepEqual(result.problems, want) {
		t.Fatalf("problems = %#v, want %#v", result.problems, want)
	}
	if _, err := os.Stat(filepath.Join(repo, "rust", "src", "engine.rs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check repaired a missing artifact: %v", err)
	}
}

func TestCheckGeneratedHonorsTargetSelection(t *testing.T) {
	repo := createFixtureRepository(t, testTempDir(t))
	all, _ := parseTargets(nil)
	seedArtifacts(t, repo, artifactsFor(all), true)
	writeTestFile(t, filepath.Join(repo, "zig", "src", "engine.zig"), []byte("stale\n"))
	result, err := executeWorkflow(repo, targetSet{"rust": true}, true, successfulFakeRunner(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.problems) != 0 {
		t.Fatalf("unselected Zig artifact affected Rust check: %#v", result.problems)
	}
}

func TestRunExitCodesAndDiagnostics(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := successfulFakeRunner()
	if code := run([]string{"-unknown"}, ".", &stdout, &stderr, runner); code != 2 {
		t.Fatalf("unknown flag exit = %d, want 2", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-target", "Rust"}, ".", &stdout, &stderr, runner); code != 2 {
		t.Fatalf("bad target exit = %d, want 2", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"operand"}, ".", &stdout, &stderr, runner); code != 2 {
		t.Fatalf("positional argument exit = %d, want 2", code)
	}
	stdout.Reset()
	stderr.Reset()
	actualRepo, err := conformance.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	if code := run(nil, filepath.Dir(actualRepo), &stdout, &stderr, runner); code != 1 {
		t.Fatalf("root discovery exit = %d, want 1", code)
	}

	repo := createFixtureRepository(t, testTempDir(t))
	targets := targetSet{"rust": true}
	seedArtifacts(t, repo, artifactsFor(targets), false)
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-check", "-repo", repo, "-target", "rust"}, repo,
		&stdout, &stderr, successfulFakeRunner()); code != 1 {
		t.Fatalf("stale check exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "stale: revera.vego.json") ||
		!strings.Contains(stderr.String(), "go run ./cmd/generate -target rust") {
		t.Fatalf("stale diagnostics are not actionable: %q", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-repo", repo, "-target", "rust"}, repo,
		&stdout, &stderr, successfulFakeRunner()); code != 0 {
		t.Fatalf("generate exit = %d, want 0; stderr %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-check", "-repo", repo, "-target", "rust"}, repo,
		&stdout, &stderr, successfulFakeRunner()); code != 0 {
		t.Fatalf("clean check exit = %d, want 0; stderr %q", code, stderr.String())
	}
}

func TestRepositoryGeneratedArtifactsAreCurrent(t *testing.T) {
	repo, err := conformance.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	targets, err := parseTargets(nil)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	result, err := executeWorkflow(repo, targets, true, goRunner{}, &stderr)
	if err != nil {
		t.Fatalf("check generated artifacts: %v\n%s", err, stderr.String())
	}
	if len(result.problems) != 0 {
		t.Fatalf("generated artifacts are stale: %#v", result.problems)
	}
}

func assertArtifactContents(t *testing.T, repo string, want map[string][]byte) {
	t.Helper()
	for rel, expected := range want {
		got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			continue
		}
		if !bytes.Equal(got, expected) {
			t.Errorf("%s changed: got %q, want %q", rel, got, expected)
		}
	}
}
