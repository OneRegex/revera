// Package bench runs the shared benchmark cases against every engine and prints the results side by side.
// The Go engine runs in-process.
// Every other backend runs its bench binary, named in its backend.json, over the same commands.
// The size report measures the generated sources and the engine code in the release drivers.
package bench

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/oneregex/revera/dev/internal/conformance"
	"github.com/oneregex/revera/dev/internal/protocol"
	"github.com/oneregex/revera/go"
)

const usageText = `usage:
  bench [-repo path] [-backend dir]... [-reps n] [-scale f] [-reference] [-build] [-only prefix] [-tsv file]
  bench size [-repo path] [-backend dir]...

The first form times the shared cases on the Go engine and on every backend bench binary.
The second form reports the size of the generated sources and of the engine code in the release drivers.`

// measurement is what one engine reported for one case.
type measurement struct {
	nsPerOp float64
	maxNs   float64
	bytes   int64
	allocs  int64
	code    int32
}

// column is one engine under measurement, with its measurement per case key.
type column struct {
	name string
	by   map[string]measurement
}

// commonFlags declares the flags that both forms of the command take.
func commonFlags(flags *flag.FlagSet) (repo *string, backends *[]string) {
	repo = flags.String("repo", "", "repository root (default: discover from the working directory)")
	var values []string
	flags.Func("backend", "backend directory or manifest; repeatable", func(v string) error {
		values = append(values, v)
		return nil
	})
	return repo, &values
}

// Main runs the command line and returns the exit status.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "size" {
		return runSize(args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("bench", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprintln(stderr, usageText) }
	repoValue, backendValues := commonFlags(flags)
	reps := flags.Int("reps", 5, "timed repetitions per case; the table shows the fastest")
	scale := flags.Float64("scale", 1, "multiplier on the iteration counts of the cases")
	withReference := flags.Bool("reference", false, "also time the reference engine of dev/internal/reference")
	buildFirst := flags.Bool("build", false, "run the release build of each backend first")
	only := flags.String("only", "", "run only the cases whose group/name starts with this prefix")
	tsvPath := flags.String("tsv", "", "also write every measurement to this tab-separated file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, usageText)
		return 2
	}
	if *reps < 1 || *scale <= 0 {
		fmt.Fprintln(stderr, "-reps must be at least 1 and -scale must be positive")
		return 2
	}
	repo, backends, err := resolve(*repoValue, *backendValues)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cases := selectCases(*only, *scale)
	if len(cases) == 0 {
		fmt.Fprintln(stderr, "no case matches", *only)
		return 1
	}
	// Every manifest is checked and every backend built before the first measurement.
	// The timed columns then run back to back, on a machine in the same state.
	for _, b := range backends {
		if b.Release.Bench == "" {
			fmt.Fprintf(stderr, "%s: backend.json names no bench binary\n", b.Name)
			return 1
		}
		if *buildFirst {
			if out, err := conformance.BuildBinaries(b, b.Release); err != nil {
				fmt.Fprintf(stderr, "%s: %v\n%s", b.Name, err, out)
				return 1
			}
		}
	}
	commands := benchCommands(cases, *reps)
	printHeader(stdout, repo, backends, cases, *reps, *scale)
	columns := []column{{name: "go", by: evalInProcess(protocol.NewBenchSession(), commands, cases, *reps)}}
	if *withReference {
		columns = append(columns, column{name: "reference", by: evalInProcess(newReferenceSession(), commands, cases, *reps)})
	}
	for _, b := range backends {
		by, err := evalBinary(b.Path(b.Release.Bench), commands, cases, *reps)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", b.Name, err)
			return 1
		}
		columns = append(columns, column{name: b.Name, by: by})
	}
	contracts := contractsFor(cases)
	printTables(stdout, cases, columns, contracts)
	if *tsvPath != "" {
		if err := writeTSV(*tsvPath, cases, columns, contracts, *reps); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "measurements written to %s\n", *tsvPath)
	}
	return 0
}

// resolve finds the repository and loads the selected manifests.
func resolve(repoValue string, backendValues []string) (string, []*conformance.Backend, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	repo, err := conformance.ResolveRepositoryRoot(repoValue, cwd)
	if err != nil {
		return "", nil, err
	}
	backends, err := conformance.SelectBackends(repo, cwd, backendValues)
	if err != nil {
		return "", nil, err
	}
	return repo, backends, nil
}

// selectCases keeps the cases whose key starts with the prefix, with their iteration counts scaled.
func selectCases(only string, scale float64) []protocol.BenchCase {
	var out []protocol.BenchCase
	for _, c := range protocol.BenchCases() {
		if strings.HasPrefix(c.Key(), only) {
			c.Iters = max(1, int(float64(c.Iters)*scale))
			out = append(out, c)
		}
	}
	return out
}

// benchCommands builds the protocol lines: a locale command, then the B command, per case.
func benchCommands(cases []protocol.BenchCase, reps int) []string {
	var lines []string
	for _, c := range cases {
		if c.Locale == "" {
			lines = append(lines, "P")
		} else {
			lines = append(lines, "L "+protocol.DriverEncode(c.Locale)+" -")
		}
		lines = append(lines, protocol.BenchCommand(c, reps))
	}
	return lines
}

type session interface {
	Eval(line string) string
}

func evalInProcess(s session, commands []string, cases []protocol.BenchCase, reps int) map[string]measurement {
	answers := make([]string, len(commands))
	for i, line := range commands {
		answers[i] = s.Eval(line)
	}
	by, err := collect(commands, answers, cases, reps)
	if err != nil {
		panic(err)
	}
	return by
}

func evalBinary(path string, commands []string, cases []protocol.BenchCase, reps int) (map[string]measurement, error) {
	answers, err := conformance.RunProtocol(path, strings.Join(commands, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	if len(answers) != len(commands) {
		return nil, fmt.Errorf("%s answered %d lines to %d commands", path, len(answers), len(commands))
	}
	return collect(commands, answers, cases, reps)
}

// collect pairs every answer with its command, checks the shape of each B answer, and reduces it to a measurement.
func collect(commands, answers []string, cases []protocol.BenchCase, reps int) (map[string]measurement, error) {
	iters := map[string]int{}
	for _, c := range cases {
		iters[c.Key()] = c.Iters
	}
	by := map[string]measurement{}
	for i, line := range commands {
		switch line[0] {
		case 'P':
			if answers[i] != "P 1" {
				return nil, fmt.Errorf("command %q answered %q", line, answers[i])
			}
		case 'L':
			if answers[i] != "L 1" {
				return nil, fmt.Errorf("command %q answered %q", line, answers[i])
			}
		case 'B':
			r, err := protocol.ParseBenchResult(answers[i])
			if err != nil {
				return nil, err
			}
			if want := strings.Fields(line)[1]; r.Name != want {
				return nil, fmt.Errorf("command for %s answered for %s", want, r.Name)
			}
			if r.Code == 0 && len(r.Nanos) != reps {
				return nil, fmt.Errorf("%s answered %d timings, want %d", r.Name, len(r.Nanos), reps)
			}
			by[r.Name] = measure(r, iters[r.Name])
		}
	}
	return by, nil
}

func measure(r protocol.BenchResult, iters int) measurement {
	m := measurement{bytes: r.Bytes, allocs: r.Allocs, code: r.Code}
	for i, ns := range r.Nanos {
		v := float64(ns) / float64(iters)
		if i == 0 || v < m.nsPerOp {
			m.nsPerOp = v
		}
		m.maxNs = max(m.maxNs, v)
	}
	return m
}

// contractsFor computes the contract heap bound of every match case, for the subject length of the case.
func contractsFor(cases []protocol.BenchCase) map[string]int64 {
	base := protocol.MustEmbeddedLocale()
	out := map[string]int64{}
	for _, c := range cases {
		if c.Kind != protocol.BenchMatch {
			continue
		}
		loc, ok := protocol.LocaleByName(&base, c.Locale)
		if !ok {
			continue
		}
		re, err := revera.Compile(c.Pattern, loc, c.Flags)
		if err.Code != revera.ErrNone {
			continue
		}
		contract := revera.ContractFor(&re, len(c.Subject))
		out[c.Key()] = revera.ContractHeapBytes(&contract)
	}
	return out
}

func printHeader(w io.Writer, repo string, backends []*conformance.Backend, cases []protocol.BenchCase, reps int, scale float64) {
	fmt.Fprintf(w, "revera benchmarks, %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(w, "machine: %s/%s, %s, %d cpus\n", runtime.GOOS, runtime.GOARCH, cpuModel(), runtime.NumCPU())
	fmt.Fprintf(w, "toolchains: %s\n", strings.Join(toolchains(backends), "; "))
	fmt.Fprintf(w, "%d cases, %d repetitions each, iteration scale %g; the tables show the fastest repetition\n", len(cases), reps, scale)
	fmt.Fprintf(w, "repository: %s\n\n", repo)
}

func cpuModel() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "model name") {
				_, value, _ := strings.Cut(line, ":")
				return strings.TrimSpace(value)
			}
		}
	}
	return "unknown cpu"
}

// toolchains lists the Go version and the first line of each backend's toolchain command.
func toolchains(backends []*conformance.Backend) []string {
	out := []string{runtime.Version()}
	for _, b := range backends {
		if len(b.Toolchain) == 0 {
			continue
		}
		raw, err := exec.Command(b.Toolchain[0], b.Toolchain[1:]...).Output()
		if err != nil {
			continue
		}
		line, _, _ := strings.Cut(strings.TrimSpace(string(raw)), "\n")
		out = append(out, b.Name+": "+line)
	}
	return out
}

func printTables(w io.Writer, cases []protocol.BenchCase, columns []column, contracts map[string]int64) {
	for _, g := range protocol.BenchGroups {
		header := []string{"case"}
		for _, col := range columns {
			header = append(header, col.name)
		}
		rows := [][]string{header}
		for _, c := range cases {
			if c.Group != g.Name {
				continue
			}
			row := []string{c.Name}
			for _, col := range columns {
				row = append(row, formatNs(col.by[c.Key()]))
			}
			rows = append(rows, row)
		}
		if len(rows) == 1 {
			continue
		}
		fmt.Fprintln(w, g.Title)
		conformance.WriteTable(w, rows, true)
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "allocation per operation, bytes and count; contract is the heap bound of the pattern for the subject length")
	header := []string{"case"}
	for _, col := range columns {
		header = append(header, col.name+" B/op", col.name+" allocs")
	}
	header = append(header, "contract B")
	rows := [][]string{header}
	for _, c := range cases {
		row := []string{c.Key()}
		for _, col := range columns {
			m := col.by[c.Key()]
			if m.code != 0 {
				row = append(row, "err", "err")
				continue
			}
			row = append(row, fmt.Sprint(m.bytes), fmt.Sprint(m.allocs))
		}
		if v, ok := contracts[c.Key()]; ok {
			row = append(row, fmt.Sprint(v))
		} else {
			row = append(row, "")
		}
		rows = append(rows, row)
	}
	conformance.WriteTable(w, rows, true)
}

func formatNs(m measurement) string {
	if m.code != 0 {
		return fmt.Sprintf("error %d", m.code)
	}
	switch {
	case m.nsPerOp >= 1e6:
		return fmt.Sprintf("%.2f ms", m.nsPerOp/1e6)
	case m.nsPerOp >= 1e3:
		return fmt.Sprintf("%.1f us", m.nsPerOp/1e3)
	default:
		return fmt.Sprintf("%.0f ns", m.nsPerOp)
	}
}

func writeTSV(path string, cases []protocol.BenchCase, columns []column, contracts map[string]int64, reps int) error {
	var b strings.Builder
	b.WriteString("group\tcase\tengine\tns_per_op\tmax_ns_per_op\tbytes_per_op\tallocs_per_op\tcontract_bytes\titers\treps\tcode\n")
	for _, c := range cases {
		for _, col := range columns {
			m := col.by[c.Key()]
			fmt.Fprintf(&b, "%s\t%s\t%s\t%.1f\t%.1f\t%d\t%d\t%d\t%d\t%d\t%d\n",
				c.Group, c.Name, col.name, m.nsPerOp, m.maxNs, m.bytes, m.allocs, contracts[c.Key()], c.Iters, reps, m.code)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
