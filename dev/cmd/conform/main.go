// Command conform runs the backend conformance kit.
//
// Usage:
//
//	conform [-repo path] [-backend dir]... [-stress rounds] [-seed n] [-quick] [-skip steps] [-lean] [-allow-skip] [-timeout d]
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oneregex/revera/dev/internal/conformance"
)

var conformUsage = `usage:
  conform [-repo path] [-backend dir]... [-stress rounds] [-seed n] [-quick] [-skip steps] [-lean] [-allow-skip] [-timeout d]

Runs the backend conformance kit.
Without -backend, every directory with a backend.json below the repository root is checked.
-skip takes a comma-separated list among ` + strings.Join(conformance.StepNames, ", ") + `.
The exit status is 0 only when every step passed; -allow-skip also accepts skipped steps.`

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(run(os.Args[1:], cwd, os.Stdout, os.Stderr))
}

func run(args []string, cwd string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("conform", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprintln(stderr, conformUsage) }
	var repoValue string
	var backendValues []string
	var skipValue string
	stress := flags.Int64("stress", 20, "extra random rounds of 500 patterns for the release driver")
	seed := flags.Int64("seed", conformance.DefaultExtraSeed, "seed of the first stress round")
	quick := flags.Bool("quick", false, "shrink every random corpus block by ten")
	lean := flags.Bool("lean", false, "also run the Lean build, the corpus replay, and the specification check")
	allowSkip := flags.Bool("allow-skip", false, "exit 0 when steps were skipped but none failed")
	timeout := flags.Duration("timeout", conformance.ProtocolTimeout, "time limit of one driver, probe, or fuzzcase run")
	flags.StringVar(&repoValue, "repo", "", "repository root (default: discover from the working directory)")
	flags.Func("backend", "backend directory or manifest; repeatable", func(value string) error {
		backendValues = append(backendValues, value)
		return nil
	})
	flags.StringVar(&skipValue, "skip", "", "comma-separated steps to leave out")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "conform does not accept positional arguments")
		return 2
	}
	if *stress < 0 || *seed < 0 {
		fmt.Fprintln(stderr, "-stress and -seed must not be negative")
		return 2
	}
	if *timeout <= 0 {
		fmt.Fprintln(stderr, "-timeout must be positive")
		return 2
	}
	conformance.ProtocolTimeout = *timeout
	repo, err := conformance.ResolveRepositoryRoot(repoValue, cwd)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	skip, err := conformance.ParseSkips(skipValue)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	backends, err := conformance.SelectBackends(repo, cwd, backendValues)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	summary := conformance.Run(backends, conformance.Options{
		Repo:     repo,
		Stress:   *stress,
		Seed:     *seed,
		Quick:    *quick,
		Skip:     skip,
		Lean:     *lean,
		Progress: stdout,
	})
	fmt.Fprintln(stdout)
	summary.WriteTable(stdout)
	for _, b := range backends {
		fmt.Fprintln(stdout, summary.Verdict(b.Name))
	}
	if summary.Failed() {
		return 1
	}
	if summary.Skips() > 0 && !*allowSkip {
		fmt.Fprintln(stdout, "some steps were skipped; pass -allow-skip to accept that")
		return 1
	}
	return 0
}
