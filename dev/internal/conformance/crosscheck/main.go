// Command crosscheck checks the target-language instantiations of the revera engine against the Go engine.
// It generates the same corpora as the Go differential tests, and encodes them as driver protocol commands.
// It computes the expected output with the Go engine in-process.
// It then diffs the output of each driver line by line.
// The conformance package holds the corpus and the runner; this command is its classic entry point.
//
// Usage:
//
//	crosscheck [-quick] [-extra rounds] [-dump corpus.txt] [-dumpexpected corpus.tsv] driver-binary...
package main

import (
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/oneregex/revera/dev/internal/conformance"
)

func main() {
	quick := flag.Bool("quick", false, "run a reduced corpus")
	dump := flag.String("dump", "", "write the corpus commands to a file")
	dumpExpected := flag.String("dumpexpected", "",
		"write command and expected output pairs, tab separated")
	extra := flag.Int64("extra", 0, "additional random rounds, 500 patterns each")
	flag.Parse()
	if !hasVerificationTarget(flag.NArg(), *dump, *dumpExpected) {
		fmt.Fprintln(os.Stderr, "usage: crosscheck [-quick] [-extra rounds] [-dump corpus.txt] [-dumpexpected corpus.tsv] driver-binary...")
		os.Exit(2)
	}
	c := conformance.BuildCorpus(conformance.CorpusOptions{
		Quick: *quick, Extra: *extra, ExtraSeed: conformance.DefaultExtraSeed,
	})
	if *dump != "" {
		if err := os.WriteFile(*dump, []byte(c.Input()), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Printf("corpus: %d commands\n", len(c.Commands))
	if *dumpExpected != "" {
		if err := os.WriteFile(*dumpExpected, []byte(c.Dump()), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	// The drivers are independent processes.
	// They run together, and the report follows argument order once all of them finish.
	reports := make([]conformance.Report, flag.NArg())
	var wg sync.WaitGroup
	for di, driver := range flag.Args() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reports[di] = conformance.RunDriver(driver, c)
		}()
	}
	wg.Wait()
	failed := false
	for _, r := range reports {
		fmt.Print(r.Text)
		failed = failed || r.Failed
	}
	if failed {
		os.Exit(1)
	}
}

func hasVerificationTarget(drivers int, dump, dumpExpected string) bool {
	return drivers > 0 || dump != "" || dumpExpected != ""
}
