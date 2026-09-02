package reference

// CompileFlags select compilation behavior.
// The library implements only the ERE language, so REG_EXTENDED is implicit and has no flag here.
type CompileFlags uint32

const (
	// ICase applies the case-insensitive closure of section 10.2.
	ICase CompileFlags = 1 << iota
	// Newline applies the four newline exceptions of section 12.3.
	Newline
	// NoSub compiles for success or failure reporting only.
	NoSub
	// Minimal makes every duplication shortest-preferring by default.
	// A repetition modifier then reverses one duplication back.
	Minimal
)

// ExecFlags select execution behavior.
type ExecFlags uint32

const (
	// NotBOL removes the line boundary before the first character.
	NotBOL ExecFlags = 1 << iota
	// NotEOL removes the line boundary after the last character.
	NotEOL
)

// DupMax is the largest supported interval count.
// POSIX requires at least 255.
const DupMax = 255
