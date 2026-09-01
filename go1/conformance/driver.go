package conformance

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"revera1/probe"
)

// Report is the outcome of one binary against its reference.
type Report struct {
	// Text is the human-readable report, ending with an OK or FAIL line.
	Text string
	// Failed is true when any line differed or the binary did not run.
	Failed bool
}

// ProtocolTimeout bounds one driver, probe, or fuzzcase run, so a wedged binary fails its step instead of holding the kit.
// The conform command sets it from -timeout.
var ProtocolTimeout = 30 * time.Minute

// RunProtocol feeds a binary its input on stdin and returns its output lines.
// A trailing carriage return is dropped, so a binary that writes CRLF text is compared on its content.
func RunProtocol(bin string, input string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ProtocolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.WaitDelay = 5 * time.Second
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: no result after %s", bin, ProtocolTimeout)
		}
		return nil, fmt.Errorf("%s: %w", bin, err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines, nil
}

// RunDriver feeds a driver the corpus and diffs its output line by line.
func RunDriver(driver string, c Corpus) Report {
	return runAndDiff(driver, c.Input(), c.Commands, c.Expected)
}

// RunProbe runs a probe binary and diffs its output against the Go probe package.
func RunProbe(bin string) Report {
	return runAndDiff(bin, "", nil, probe.ReportLines())
}

func runAndDiff(bin string, input string, cmds []string, expected []string) Report {
	var b strings.Builder
	got, err := RunProtocol(bin, input)
	if err != nil {
		fmt.Fprintf(&b, "%s: FAILED to run: %v\n", bin, err)
		return Report{Text: b.String(), Failed: true}
	}
	bad := 0
	for i := range expected {
		if i >= len(got) {
			fmt.Fprintf(&b, "%s: output truncated at line %d\n", bin, i+1)
			bad++
			break
		}
		if got[i] != expected[i] {
			if bad < 10 {
				if cmds != nil {
					fmt.Fprintf(&b, "%s: line %d\n  cmd:  %s\n  want: %s\n  got:  %s\n",
						bin, i+1, cmds[i], expected[i], got[i])
				} else {
					fmt.Fprintf(&b, "%s: line %d\n  want: %s\n  got:  %s\n",
						bin, i+1, expected[i], got[i])
				}
			}
			bad++
		}
	}
	if len(got) > len(expected) {
		fmt.Fprintf(&b, "%s: %d extra output lines\n", bin, len(got)-len(expected))
		bad++
	}
	if bad > 0 {
		fmt.Fprintf(&b, "%s: FAIL (%d mismatched lines)\n", bin, bad)
		return Report{Text: b.String(), Failed: true}
	}
	fmt.Fprintf(&b, "%s: OK (%d lines)\n", bin, len(expected))
	return Report{Text: b.String()}
}
