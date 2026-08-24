package revera

import "strconv"

// Code is a POSIX regex status code. The zero value means success.
type Code int

// These values mirror the <regex.h> error constants.
const (
	NoMatch  Code = iota + 1 // regexec() found no match
	BadPat                   // invalid regular expression
	ECollate                 // invalid collating element reference
	ECType                   // invalid character class reference
	EEscape                  // trailing backslash
	ESubReg                  // invalid backreference; unused in ERE
	EBrack                   // bracket imbalance
	EParen                   // parenthesis imbalance
	EBrace                   // brace imbalance
	BadBR                    // invalid interval content
	ERange                   // invalid range endpoint
	ESpace                   // insufficient memory or capacity
	BadRpt                   // repetition without a valid operand
)

var codeText = map[Code]string{
	NoMatch:  "no match",
	BadPat:   "invalid regular expression",
	ECollate: "invalid collating element",
	ECType:   "invalid character class",
	EEscape:  "invalid or trailing backslash",
	ESubReg:  "invalid backreference",
	EBrack:   "unbalanced bracket",
	EParen:   "unbalanced parenthesis",
	EBrace:   "unbalanced brace",
	BadBR:    "invalid interval",
	ERange:   "invalid range endpoint",
	ESpace:   "capacity limit reached",
	BadRpt:   "repetition without an operand",
}

// String returns a short printable message for the code.
func (c Code) String() string {
	if text, ok := codeText[c]; ok {
		return text
	}
	return "error " + strconv.Itoa(int(c))
}

// Error reports a compilation or execution failure.
type Error struct {
	Code Code
	// Pos is the byte offset in the pattern where compilation failed,
	// or -1 when the error has no position.
	Pos int
}

func (e *Error) Error() string {
	if e.Pos >= 0 {
		return e.Code.String() + " at offset " + strconv.Itoa(e.Pos)
	}
	return e.Code.String()
}

func compileError(code Code, pos int) *Error {
	return &Error{Code: code, Pos: pos}
}
