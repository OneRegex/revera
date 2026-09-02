//go:build cgo && unix

// Package libcre wraps the POSIX regcomp and regexec of the host C library, for differential testing.
// It is not part of the matcher.
package libcre

/*
#include <regex.h>
#include <stdlib.h>
*/
import "C"

import "unsafe"

// Result holds one regexec outcome.
// Compiled reports whether regcomp succeeded.
type Result struct {
	Compiled bool
	Matched  bool
	Spans    [][2]int
}

// Run compiles pattern with REG_EXTENDED, then runs it on subject with nmatch capture slots.
func Run(pattern, subject string, nmatch int) Result {
	cpat := C.CString(pattern)
	defer C.free(unsafe.Pointer(cpat))
	var re C.regex_t
	if C.regcomp(&re, cpat, C.REG_EXTENDED) != 0 {
		return Result{}
	}
	defer C.regfree(&re)
	csub := C.CString(subject)
	defer C.free(unsafe.Pointer(csub))
	if nmatch < 1 {
		nmatch = 1
	}
	m := make([]C.regmatch_t, nmatch)
	rc := C.regexec(&re, csub, C.size_t(nmatch), &m[0], 0)
	out := Result{Compiled: true}
	if rc != 0 {
		return out
	}
	out.Matched = true
	count := min(nmatch, int(re.re_nsub)+1)
	for idx := range count {
		out.Spans = append(out.Spans,
			[2]int{int(m[idx].rm_so), int(m[idx].rm_eo)})
	}
	return out
}
