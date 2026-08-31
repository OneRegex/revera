package conformance

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"revera1/probe"
	"revera1/revera"
)

// StepNames lists the steps in the order the kit runs them, and the names that Options.Skip accepts.
// The build steps apply to the release build and to every checked build alike.
var StepNames = []string{"generated", "build", "probe", "corpus", "stress", "fuzz", "checked", "lean-data", "lean"}

// Options selects what the kit runs.
type Options struct {
	// Repo is the repository root.
	Repo string
	// Stress is the number of extra random rounds of 500 patterns for the release driver.
	Stress int64
	// Seed is the seed of the first stress round.
	Seed int64
	// Quick shrinks the random blocks of the release corpus by ten, for a fast smoke run.
	// The checked builds always run the quick and light corpus.
	Quick bool
	// Skip holds the steps to leave out, by name from StepNames.
	Skip map[string]bool
	// Lean also runs the Lean build and the corpus replay.
	Lean bool
	// Progress receives one line per finished step.
	Progress io.Writer
}

// Status is the outcome of one step.
type Status string

const (
	Passed  Status = "ok"
	Failed  Status = "FAIL"
	Skipped Status = "skip"
)

// repoScope is the backend column of the steps that belong to the repository, not to one backend.
const repoScope = "repo"

// Result is the outcome of one step of the kit.
type Result struct {
	// Backend is the backend name, or "repo" for a repository-level step.
	Backend  string
	Step     string
	Status   Status
	Duration time.Duration
	// Detail explains a failure or a skip.
	Detail string
}

// Summary is the outcome of a whole run, in report order.
type Summary []Result

// Failed reports whether any step failed.
func (s Summary) Failed() bool {
	return slices.ContainsFunc(s, func(r Result) bool { return r.Status == Failed })
}

// Skips counts the skipped steps.
func (s Summary) Skips() int {
	n := 0
	for _, r := range s {
		if r.Status == Skipped {
			n++
		}
	}
	return n
}

// Verdict states whether a backend is conformant, from its own steps and the repository-level ones.
func (s Summary) Verdict(backend string) string {
	var failed, skipped []string
	for _, r := range s {
		if r.Backend != backend && r.Backend != repoScope {
			continue
		}
		switch r.Status {
		case Failed:
			failed = append(failed, r.Step)
		case Skipped:
			skipped = append(skipped, r.Step)
		}
	}
	if len(failed) > 0 {
		return fmt.Sprintf("%s: NOT conformant (%s)", backend, strings.Join(failed, ", "))
	}
	if len(skipped) > 0 {
		return fmt.Sprintf("%s: conformant on the steps that ran; skipped %s", backend, strings.Join(skipped, ", "))
	}
	return backend + ": conformant"
}

// WriteTable prints the results as a plain-text table.
func (s Summary) WriteTable(w io.Writer) {
	rows := [][]string{{"backend", "step", "result", "time", "detail"}}
	for _, r := range s {
		detail := r.Detail
		if i := strings.IndexByte(detail, '\n'); i >= 0 {
			detail = detail[:i]
		}
		if len(detail) > 60 {
			detail = detail[:57] + "..."
		}
		rows = append(rows, []string{r.Backend, r.Step, string(r.Status), formatDuration(r.Duration), detail})
	}
	WriteTable(w, rows, false)
}

// WriteTable prints rows with -, | and + borders; the first row is the header.
// With alignRight, every column but the first is right-aligned, for numbers.
func WriteTable(w io.Writer, rows [][]string, alignRight bool) {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], len(cell))
		}
	}
	line := "+"
	for _, wd := range widths {
		line += strings.Repeat("-", wd+2) + "+"
	}
	fmt.Fprintln(w, line)
	for i, row := range rows {
		fmt.Fprint(w, "|")
		for j, cell := range row {
			if alignRight && j > 0 {
				fmt.Fprintf(w, " %*s |", widths[j], cell)
			} else {
				fmt.Fprintf(w, " %-*s |", widths[j], cell)
			}
		}
		fmt.Fprintln(w)
		if i == 0 {
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintln(w, line)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// ParseSkips turns a comma-separated list of step names into the Skip set of Options.
func ParseSkips(value string) (map[string]bool, error) {
	skip := map[string]bool{}
	if strings.TrimSpace(value) == "" {
		return skip, nil
	}
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if !slices.Contains(StepNames, name) {
			return nil, fmt.Errorf("unknown step %q", name)
		}
		skip[name] = true
	}
	return skip, nil
}

type run struct {
	opts Options
	// full, quick and stress are nil when no step needs them.
	full   *Corpus
	quick  *Corpus
	stress *Corpus
	seeds  [][]byte
	pack   string
	mu     sync.Mutex
}

// record builds the result of one step and reports it on the progress writer.
func (r *run) record(backend, step string, status Status, start time.Time, detail string) Result {
	res := Result{Backend: backend, Step: step, Status: status, Duration: time.Since(start), Detail: detail}
	if r.opts.Progress != nil {
		r.mu.Lock()
		fmt.Fprintf(r.opts.Progress, "[%s] %s: %s (%s)\n", backend, step, status, formatDuration(res.Duration))
		if status != Passed && detail != "" {
			fmt.Fprintln(r.opts.Progress, indent(detail))
		}
		r.mu.Unlock()
	}
	return res
}

// report records the outcome of a binary against its reference.
func (r *run) report(backend, step string, start time.Time, rep Report) Result {
	if rep.Failed {
		return r.record(backend, step, Failed, start, strings.TrimRight(rep.Text, "\n"))
	}
	return r.record(backend, step, Passed, start, "")
}

func indent(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := range lines {
		lines[i] = "    " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (r *run) skip(name string) bool {
	return r.opts.Skip[name]
}

// Run drives every step for the given backends and returns the outcomes.
// The backends run concurrently; the results keep manifest order.
func Run(backends []*Backend, opts Options) Summary {
	r := &run{opts: opts}
	var out Summary
	if !r.skip("generated") {
		out = append(out, r.checkGenerated())
	}
	out = append(out, r.prepare(backends))
	perBackend := make([]Summary, len(backends))
	var wg sync.WaitGroup
	for i, b := range backends {
		wg.Add(1)
		go func() {
			defer wg.Done()
			perBackend[i] = r.backend(b)
		}()
	}
	wg.Wait()
	for _, s := range perBackend {
		out = append(out, s...)
	}
	if !r.skip("lean-data") {
		out = append(out, r.leanData())
	}
	if opts.Lean && !r.skip("lean") {
		out = append(out, r.lean())
	}
	return out
}

func (r *run) checkGenerated() Result {
	start := time.Now()
	out, err := command(filepath.Join(r.opts.Repo, "go1"), "go", "run", "./cmd/revera", "check-generated")
	if err != nil {
		return r.record(repoScope, "generated", Failed, start, tail(out, 12))
	}
	return r.record(repoScope, "generated", Passed, start, "")
}

// prepare answers the corpora that the requested steps need and writes the fuzz seed pack.
func (r *run) prepare(backends []*Backend) Result {
	start := time.Now()
	checked := false
	for _, b := range backends {
		checked = checked || len(b.Checked) > 0
	}
	if !r.skip("corpus") {
		full := BuildCorpus(CorpusOptions{Quick: r.opts.Quick})
		r.full = &full
		if checked && !r.skip("checked") {
			quick := BuildCorpus(CorpusOptions{Quick: true, Light: true})
			r.quick = &quick
		}
	}
	if !r.skip("stress") && r.opts.Stress > 0 {
		stress := Answer(StressLines(r.opts.Seed, r.opts.Stress, r.opts.Quick))
		r.stress = &stress
	}
	if !r.skip("fuzz") {
		r.seeds = revera.FuzzSeeds()
		dir := filepath.Join(r.opts.Repo, "tmp", "conformance")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return r.record(repoScope, "corpus", Failed, start, err.Error())
		}
		r.pack = filepath.Join(dir, "fuzz-seeds.pack")
		var buf bytes.Buffer
		if err := revera.WriteFuzzPack(&buf, r.seeds); err != nil {
			return r.record(repoScope, "corpus", Failed, start, err.Error())
		}
		if err := os.WriteFile(r.pack, buf.Bytes(), 0o644); err != nil {
			return r.record(repoScope, "corpus", Failed, start, err.Error())
		}
	}
	return r.record(repoScope, "corpus", Passed, start, fmt.Sprintf("%d commands, %d stress commands, %d fuzz seeds",
		commandCount(r.full), commandCount(r.stress), len(r.seeds)))
}

func commandCount(c *Corpus) int {
	if c == nil {
		return 0
	}
	return len(c.Commands)
}

// backend runs the release build through every step, then each checked build through the same steps.
func (r *run) backend(b *Backend) Summary {
	out := r.build(b, b.Release, "", r.full, r.stress)
	if len(out) > 0 && out[0].Status != Passed {
		return out
	}
	if !r.skip("checked") {
		for _, c := range b.Checked {
			out = append(out, r.build(b, c, "checked/"+c.Name+"/", r.quick, nil)...)
		}
	}
	return out
}

// build runs one build of a backend: build, probe, corpus, stress, fuzz.
// The step names carry the prefix, so a checked build reports as checked/<name>/<step>.
func (r *run) build(b *Backend, bd Build, prefix string, corpus *Corpus, stress *Corpus) Summary {
	var out Summary
	if !r.skip("build") {
		start := time.Now()
		if len(bd.Requires) > 0 {
			if msg, err := command(b.Dir, bd.Requires[0], bd.Requires[1:]...); err != nil {
				return Summary{r.record(b.Name, prefix+"build", Skipped, start,
					fmt.Sprintf("%s is not available: %s", strings.Join(bd.Requires, " "), firstLine(msg)))}
			}
		}
		if msg, err := BuildBinaries(b, bd); err != nil {
			return Summary{r.record(b.Name, prefix+"build", Failed, start, tail(msg, 12))}
		}
		out = append(out, r.record(b.Name, prefix+"build", Passed, start, ""))
	}
	if !r.skip("probe") {
		start := time.Now()
		out = append(out, r.report(b.Name, prefix+"probe", start, RunProbe(b.Path(bd.Probe))))
	}
	if corpus != nil {
		start := time.Now()
		out = append(out, r.report(b.Name, prefix+"corpus", start, RunDriver(b.Path(bd.Driver), *corpus)))
	}
	if stress != nil {
		start := time.Now()
		out = append(out, r.report(b.Name, prefix+"stress", start, RunDriver(b.Path(bd.Driver), *stress)))
	}
	if !r.skip("fuzz") {
		start := time.Now()
		switch err := r.fuzzcase(b, bd); {
		case bd.Fuzzcase == "":
			out = append(out, r.record(b.Name, prefix+"fuzz", Skipped, start, "the manifest names no fuzzcase binary"))
		case err != nil:
			out = append(out, r.record(b.Name, prefix+"fuzz", Failed, start, err.Error()))
		default:
			out = append(out, r.record(b.Name, prefix+"fuzz", Passed, start, ""))
		}
	}
	return out
}

// fuzzcase runs the seed pack through a fuzzcase binary and checks its count line.
func (r *run) fuzzcase(b *Backend, bd Build) error {
	if bd.Fuzzcase == "" {
		return nil
	}
	out, err := command(b.Dir, b.Path(bd.Fuzzcase), r.pack)
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", bd.Fuzzcase, err, tail(out, 12))
	}
	want := fmt.Sprintf("fuzzcase: %d inputs", len(r.seeds))
	if !strings.Contains(out, want) {
		return fmt.Errorf("%s did not report %q: %s", bd.Fuzzcase, want, firstLine(out))
	}
	return nil
}

// LeanDataProblems compares the Lean data files with the current corpus and probe report.
// full may hold an already answered full corpus; nil builds one.
func LeanDataProblems(repo string, full *Corpus) []string {
	var problems []string
	leanDir := filepath.Join(repo, "lean", "data")
	corpus, err := os.ReadFile(filepath.Join(leanDir, "corpus.tsv"))
	if err != nil {
		problems = append(problems, "corpus.tsv: "+err.Error())
	} else {
		if full == nil {
			c := BuildCorpus(CorpusOptions{})
			full = &c
		}
		if string(corpus) != full.Dump() {
			problems = append(problems, "corpus.tsv is stale")
		}
	}
	expected, err := os.ReadFile(filepath.Join(leanDir, "probe.expected"))
	if err != nil {
		problems = append(problems, "probe.expected: "+err.Error())
	} else if string(expected) != strings.Join(probe.ReportLines(), "\n")+"\n" {
		problems = append(problems, "probe.expected is stale")
	}
	return problems
}

func (r *run) leanData() Result {
	start := time.Now()
	var full *Corpus
	if !r.opts.Quick {
		full = r.full
	}
	if problems := LeanDataProblems(r.opts.Repo, full); len(problems) > 0 {
		return r.record(repoScope, "lean-data", Failed, start,
			strings.Join(problems, "; ")+"; regenerate with the commands in lean/README.md")
	}
	return r.record(repoScope, "lean-data", Passed, start, "")
}

func (r *run) lean() Result {
	start := time.Now()
	leanDir := filepath.Join(r.opts.Repo, "lean")
	if out, err := command(leanDir, "lake", "build"); err != nil {
		return r.record(repoScope, "lean", Failed, start, "lake build: "+tail(out, 12))
	}
	out, err := command(leanDir, filepath.Join(".lake", "build", "bin", "vegocheck"), filepath.Join("data", "corpus.tsv"))
	if err != nil {
		return r.record(repoScope, "lean", Failed, start, "vegocheck: "+tail(out, 12))
	}
	return r.record(repoScope, "lean", Passed, start, lastLine(out))
}

func tail(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimLeft(text, "\n"), "\n")
	return line
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return lines[len(lines)-1]
}
