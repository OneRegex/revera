// Command generate regenerates or checks the Vego IR and the generated target sources.
// It renders every artifact into a staging directory under tmp/ with vegoc, and installs them only when every step succeeded.
//
// Usage:
//
//	generate [-repo path] [-target rust,zig,ts,cpp,c|all]
//	generate -check [-repo path] [-target rust,zig,ts,cpp,c|all]
//
// With -check, nothing is installed; the command compares the staged output with the checked-in files and exits nonzero when one is stale or missing.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oneregex/revera/dev/internal/conformance"
)

const usageText = `usage:
  generate [-repo path] [-target rust,zig,ts,cpp,c|all]
  generate -check [-repo path] [-target rust,zig,ts,cpp,c|all]

The -target flag may be repeated. Omitting it selects all targets.
-check compares the staged output with the checked-in files and installs nothing.`

var targetOrder = []string{"rust", "zig", "ts", "cpp", "c"}

type targetValues []string

func (v *targetValues) String() string {
	return strings.Join(*v, ",")
}

func (v *targetValues) Set(value string) error {
	*v = append(*v, value)
	return nil
}

type targetSet map[string]bool

func parseTargets(values []string) (targetSet, error) {
	if len(values) == 0 {
		return targetSet{"rust": true, "zig": true, "ts": true, "cpp": true, "c": true}, nil
	}
	selected := targetSet{}
	sawAll := false
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				return nil, fmt.Errorf("empty generation target")
			}
			if name == "all" {
				sawAll = true
				continue
			}
			if !contains(targetOrder, name) {
				return nil, fmt.Errorf("unknown generation target %q", name)
			}
			selected[name] = true
		}
	}
	if sawAll {
		if len(selected) != 0 || len(values) != 1 || strings.TrimSpace(values[0]) != "all" {
			return nil, fmt.Errorf("target all cannot be combined with another target")
		}
		return targetSet{"rust": true, "zig": true, "ts": true, "cpp": true, "c": true}, nil
	}
	return selected, nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func targetArgument(targets targetSet) string {
	if len(targets) == len(targetOrder) {
		all := true
		for _, name := range targetOrder {
			all = all && targets[name]
		}
		if all {
			return "all"
		}
	}
	var names []string
	for _, name := range targetOrder {
		if targets[name] {
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}

type artifact struct {
	rel string
}

// reveraIR and probeIR are the two IR artifacts, relative to the repository root.
const (
	reveraIR = "revera.vego.json"
	probeIR  = "vego/probe/probe.vego.json"
)

func artifactsFor(targets targetSet) []artifact {
	artifacts := []artifact{
		{rel: reveraIR},
		{rel: probeIR},
	}
	if targets["rust"] {
		artifacts = append(artifacts,
			artifact{rel: "rust/src/engine.rs"},
			artifact{rel: "rust/src/probe_engine.rs"})
	}
	if targets["zig"] {
		artifacts = append(artifacts,
			artifact{rel: "zig/src/engine.zig"},
			artifact{rel: "zig/src/probe_engine.zig"})
	}
	if targets["ts"] {
		artifacts = append(artifacts,
			artifact{rel: "ts/src/engine.ts"},
			artifact{rel: "ts/src/probe_engine.ts"})
	}
	if targets["cpp"] {
		artifacts = append(artifacts,
			artifact{rel: "native/cpp/engine.hpp"},
			artifact{rel: "native/cpp/engine.cpp"},
			artifact{rel: "native/cpp/probe_engine.hpp"},
			artifact{rel: "native/cpp/probe_engine.cpp"})
	}
	if targets["c"] {
		artifacts = append(artifacts,
			artifact{rel: "native/c/engine.h"},
			artifact{rel: "native/c/engine.c"},
			artifact{rel: "native/c/probe_engine.h"},
			artifact{rel: "native/c/probe_engine.c"})
	}
	return artifacts
}

type generationStep struct {
	name    string
	args    []string
	outputs []plannedOutput
}

type plannedOutput struct {
	artifact artifact
	path     string
}

// generationPlan lists the vegoc invocations that render the selected artifacts into stage.
// Every step runs in the vego module directory, so "go run ./cmd/vegoc" resolves the toolchain of this checkout.
func generationPlan(repo, stage string, targets targetSet) []generationStep {
	staged := func(rel string) string { return filepath.Join(stage, filepath.FromSlash(rel)) }
	vegoc := func(args ...string) []string { return append([]string{"run", "./cmd/vegoc"}, args...) }
	single := func(name, target, rel, input string) generationStep {
		return generationStep{
			name:    name,
			args:    vegoc("emit", target, "-o", staged(rel), input),
			outputs: []plannedOutput{{artifact: artifact{rel: rel}, path: staged(rel)}},
		}
	}
	pair := func(name, target, header, source, input string, extra ...string) generationStep {
		args := []string{"emit", target, "-header", staged(header), "-source", staged(source)}
		args = append(args, extra...)
		args = append(args, input)
		return generationStep{
			name: name,
			args: vegoc(args...),
			outputs: []plannedOutput{
				{artifact: artifact{rel: header}, path: staged(header)},
				{artifact: artifact{rel: source}, path: staged(source)},
			},
		}
	}
	reveraJSON := staged(reveraIR)
	probeJSON := staged(probeIR)
	steps := []generationStep{
		{
			name:    "export revera IR",
			args:    vegoc("export", "-o", reveraJSON, filepath.Join(repo, "go")),
			outputs: []plannedOutput{{artifact: artifact{rel: reveraIR}, path: reveraJSON}},
		},
		{
			name:    "export probe IR",
			args:    vegoc("export", "-o", probeJSON, filepath.Join(repo, "vego", "probe")),
			outputs: []plannedOutput{{artifact: artifact{rel: probeIR}, path: probeJSON}},
		},
	}
	if targets["rust"] {
		steps = append(steps,
			single("generate Rust engine", "rust", "rust/src/engine.rs", reveraJSON),
			single("generate Rust probe", "rust", "rust/src/probe_engine.rs", probeJSON))
	}
	if targets["zig"] {
		steps = append(steps,
			single("generate Zig engine", "zig", "zig/src/engine.zig", reveraJSON),
			single("generate Zig probe", "zig", "zig/src/probe_engine.zig", probeJSON))
	}
	if targets["ts"] {
		steps = append(steps,
			single("generate TypeScript engine", "ts", "ts/src/engine.ts", reveraJSON),
			single("generate TypeScript probe", "ts", "ts/src/probe_engine.ts", probeJSON))
	}
	if targets["cpp"] {
		steps = append(steps,
			pair("generate C++ engine", "cpp", "native/cpp/engine.hpp", "native/cpp/engine.cpp", reveraJSON,
				"-namespace", "revera::engine"),
			pair("generate C++ probe", "cpp", "native/cpp/probe_engine.hpp", "native/cpp/probe_engine.cpp", probeJSON))
	}
	if targets["c"] {
		steps = append(steps,
			pair("generate C engine", "c", "native/c/engine.h", "native/c/engine.c", reveraJSON,
				"-prefix", "revera_eng"),
			pair("generate C probe", "c", "native/c/probe_engine.h", "native/c/probe_engine.c", probeJSON))
	}
	return steps
}

type stepRunner interface {
	Run(dir string, step generationStep, stderr io.Writer) error
}

type goRunner struct{}

func (goRunner) Run(dir string, step generationStep, stderr io.Writer) error {
	cmd := exec.Command("go", step.args...)
	cmd.Dir = dir
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", step.name, err)
	}
	return nil
}

func prepareStage(stage string, artifacts []artifact) error {
	for _, artifact := range artifacts {
		dir := filepath.Dir(filepath.Join(stage, filepath.FromSlash(artifact.rel)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create staging directory for %s: %w", artifact.rel, err)
		}
	}
	return nil
}

func runGeneration(repo, stage string, targets targetSet, runner stepRunner, stderr io.Writer) error {
	vego := filepath.Join(repo, "vego")
	for _, step := range generationPlan(repo, stage, targets) {
		if err := runner.Run(vego, step, stderr); err != nil {
			return err
		}
		for _, output := range step.outputs {
			info, err := os.Lstat(output.path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("%s did not create %s", step.name, output.artifact.rel)
				}
				return fmt.Errorf("inspect generated %s: %w", output.artifact.rel, err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%s created a non-regular output at %s", step.name, output.artifact.rel)
			}
		}
	}
	return nil
}

type artifactProblem struct {
	kind string
	rel  string
}

func compareArtifacts(repo, stage string, artifacts []artifact) ([]artifactProblem, error) {
	if err := validateArtifactParents(repo, artifacts); err != nil {
		return nil, err
	}
	var problems []artifactProblem
	for _, artifact := range artifacts {
		generatedPath := filepath.Join(stage, filepath.FromSlash(artifact.rel))
		info, err := os.Lstat(generatedPath)
		if err != nil {
			return nil, fmt.Errorf("inspect generated %s: %w", artifact.rel, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("generated output is not a regular file: %s", artifact.rel)
		}
		generated, err := os.ReadFile(generatedPath)
		if err != nil {
			return nil, fmt.Errorf("read generated %s: %w", artifact.rel, err)
		}
		destination := filepath.Join(repo, filepath.FromSlash(artifact.rel))
		info, err = os.Lstat(destination)
		if errors.Is(err, os.ErrNotExist) {
			problems = append(problems, artifactProblem{kind: "missing", rel: artifact.rel})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", artifact.rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			problems = append(problems, artifactProblem{kind: "symlink", rel: artifact.rel})
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("generated artifact path is not a regular file: %s", artifact.rel)
		}
		current, err := os.ReadFile(destination)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", artifact.rel, err)
		}
		if !bytes.Equal(current, generated) {
			problems = append(problems, artifactProblem{kind: "stale", rel: artifact.rel})
		}
	}
	sort.Slice(problems, func(i, j int) bool { return problems[i].rel < problems[j].rel })
	return problems, nil
}

type installedArtifact struct {
	artifact artifact
	hadOld   bool
}

type installationFileSystem interface {
	Lstat(string) (os.FileInfo, error)
	MkdirAll(string, os.FileMode) error
	Remove(string) error
	Rename(string, string) error
}

type operatingSystemFiles struct{}

func (operatingSystemFiles) Lstat(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func (operatingSystemFiles) MkdirAll(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}

func (operatingSystemFiles) Remove(path string) error {
	return os.Remove(path)
}

func (operatingSystemFiles) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

type preserveStageError struct {
	err error
}

func (e *preserveStageError) Error() string {
	return e.err.Error()
}

func (e *preserveStageError) Unwrap() error {
	return e.err
}

func preserveStage(err error) error {
	var preserved *preserveStageError
	if errors.As(err, &preserved) {
		return err
	}
	return &preserveStageError{err: err}
}

func installArtifacts(repo, stage string, artifacts []artifact,
	files installationFileSystem) (updated []string, err error) {
	problems, err := compareArtifacts(repo, stage, artifacts)
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	for _, problem := range problems {
		changed[problem.rel] = true
	}
	if len(changed) == 0 {
		return nil, nil
	}

	backupRoot := filepath.Join(stage, "backups")
	var installed []installedArtifact
	rollback := func(cause error) error {
		var rollbackErr error
		for idx := len(installed) - 1; idx >= 0; idx-- {
			entry := installed[idx]
			destination := filepath.Join(repo, filepath.FromSlash(entry.artifact.rel))
			if removeErr := files.Remove(destination); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr,
					fmt.Errorf("remove new %s during rollback: %w", entry.artifact.rel, removeErr))
			}
			if entry.hadOld {
				backup := filepath.Join(backupRoot, filepath.FromSlash(entry.artifact.rel))
				if restoreErr := files.Rename(backup, destination); restoreErr != nil {
					rollbackErr = errors.Join(rollbackErr,
						fmt.Errorf("restore %s during rollback: %w", entry.artifact.rel, restoreErr))
				}
			}
		}
		if rollbackErr != nil {
			return preserveStage(errors.Join(cause, rollbackErr))
		}
		return cause
	}

	for _, artifact := range artifacts {
		if !changed[artifact.rel] {
			continue
		}
		destination := filepath.Join(repo, filepath.FromSlash(artifact.rel))
		generated := filepath.Join(stage, filepath.FromSlash(artifact.rel))
		backup := filepath.Join(backupRoot, filepath.FromSlash(artifact.rel))
		info, statErr := files.Lstat(destination)
		hadOld := statErr == nil
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return nil, rollback(fmt.Errorf("inspect %s before installation: %w", artifact.rel, statErr))
		}
		if hadOld && info.IsDir() {
			return nil, rollback(fmt.Errorf("generated artifact path is a directory: %s", artifact.rel))
		}
		if hadOld {
			if mkdirErr := files.MkdirAll(filepath.Dir(backup), 0o755); mkdirErr != nil {
				return nil, rollback(fmt.Errorf("create backup directory for %s: %w", artifact.rel, mkdirErr))
			}
			if renameErr := files.Rename(destination, backup); renameErr != nil {
				return nil, rollback(fmt.Errorf("back up %s: %w", artifact.rel, renameErr))
			}
		}
		if renameErr := files.Rename(generated, destination); renameErr != nil {
			installErr := fmt.Errorf("install %s: %w", artifact.rel, renameErr)
			restoreFailed := false
			if hadOld {
				if restoreErr := files.Rename(backup, destination); restoreErr != nil {
					restoreFailed = true
					installErr = errors.Join(installErr,
						fmt.Errorf("restore %s after failed installation: %w", artifact.rel, restoreErr))
				}
			}
			installErr = rollback(installErr)
			if restoreFailed {
				installErr = preserveStage(installErr)
			}
			return nil, installErr
		}
		installed = append(installed, installedArtifact{artifact: artifact, hadOld: hadOld})
		updated = append(updated, artifact.rel)
	}
	return updated, nil
}

type workflowResult struct {
	artifacts []artifact
	problems  []artifactProblem
	updated   []string
}

func executeWorkflow(repo string, targets targetSet, check bool, runner stepRunner,
	stderr io.Writer) (workflowResult, error) {
	return executeWorkflowWithFiles(repo, targets, check, runner, stderr, operatingSystemFiles{})
}

func executeWorkflowWithFiles(repo string, targets targetSet, check bool, runner stepRunner,
	stderr io.Writer, files installationFileSystem) (result workflowResult, err error) {
	result.artifacts = artifactsFor(targets)
	if err := validateArtifactParents(repo, result.artifacts); err != nil {
		return result, err
	}
	tmpRoot := filepath.Join(repo, "tmp")
	if err := os.Mkdir(tmpRoot, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return result, fmt.Errorf("create repository temporary directory: %w", err)
	}
	if err := conformance.ValidateDirectoryComponents(repo, "tmp"); err != nil {
		return result, fmt.Errorf("validate repository temporary directory: %w", err)
	}
	stage, err := os.MkdirTemp(tmpRoot, "revera-generate-")
	if err != nil {
		return result, fmt.Errorf("create generation staging directory: %w", err)
	}
	cleanupStage := true
	defer func() {
		if !cleanupStage {
			return
		}
		if cleanupErr := os.RemoveAll(stage); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove generation staging directory: %w", cleanupErr))
		}
	}()
	if err := prepareStage(stage, result.artifacts); err != nil {
		return result, err
	}
	if err := runGeneration(repo, stage, targets, runner, stderr); err != nil {
		return result, err
	}
	if check {
		result.problems, err = compareArtifacts(repo, stage, result.artifacts)
		return result, err
	}
	result.updated, err = installArtifacts(repo, stage, result.artifacts, files)
	var preserved *preserveStageError
	if errors.As(err, &preserved) {
		cleanupStage = false
		err = fmt.Errorf("%w; recovery data retained in %s", err, stage)
	}
	return result, err
}

func validateArtifactParents(repo string, artifacts []artifact) error {
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		dir := filepath.Dir(filepath.FromSlash(artifact.rel))
		if dir == "." || seen[dir] {
			continue
		}
		seen[dir] = true
		if err := conformance.ValidateDirectoryComponents(repo, dir); err != nil {
			return fmt.Errorf("validate parent of %s: %w", artifact.rel, err)
		}
	}
	return nil
}

func run(args []string, cwd string, stdout, stderr io.Writer, runner stepRunner) int {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprintln(stderr, usageText) }
	var repoValue string
	var values targetValues
	check := flags.Bool("check", false, "compare the staged output with the checked-in files and install nothing")
	flags.StringVar(&repoValue, "repo", "", "repository root (default: discover from the working directory)")
	flags.Var(&values, "target", "generation target; repeat or separate with commas")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "generate does not accept positional arguments")
		return 2
	}
	targets, err := parseTargets(values)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	repo, err := conformance.ResolveRepositoryRoot(repoValue, cwd)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	result, err := executeWorkflow(repo, targets, *check, runner, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *check && len(result.problems) != 0 {
		fmt.Fprintln(stderr, "generated artifacts are not current:")
		for _, problem := range result.problems {
			fmt.Fprintf(stderr, "  %s: %s\n", problem.kind, problem.rel)
		}
		fmt.Fprintf(stderr, "run `go run ./cmd/generate -target %s` in dev/\n", targetArgument(targets))
		return 1
	}
	if len(result.updated) == 0 {
		fmt.Fprintf(stdout, "generated artifacts are current (%d files)\n", len(result.artifacts))
		return 0
	}
	sort.Strings(result.updated)
	fmt.Fprintln(stdout, "updated generated artifacts:")
	for _, rel := range result.updated {
		fmt.Fprintf(stdout, "  %s\n", rel)
	}
	fmt.Fprintf(stdout, "%d unchanged\n", len(result.artifacts)-len(result.updated))
	return 0
}

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(run(os.Args[1:], cwd, os.Stdout, os.Stderr, goRunner{}))
}
