package probe

// This package probes the Vego constructs the revera engine never
// uses, so the target printers stay correct beyond the engine:
// division overflow, byte-slice conversion, range semantics,
// partial array literals, comparable structs with string arrays,
// evaluation order, and spare-capacity zeroing. Every function is
// pure or works through a Counter borrow, so the four
// instantiations can print and compare identical results.

// minI64 is exercised through the pipeline on purpose: emitting
// MinInt64 stresses each printer's literal handling. The host
// report uses it too.
const minI64 = -9223372036854775808

// Counter records call order, for the evaluation-order probes.
type Counter struct {
	n   int
	log []int32
}

func bump(c *Counter, tag int32) int64 {
	c.n++
	c.log = append(c.log, tag)
	return int64(tag)
}

// logCode packs the recorded call order into one number.
func logCode(c *Counter) int64 {
	v := int64(0)
	for i := 0; i < len(c.log); i++ {
		v = v*10 + int64(c.log[i])
	}
	return v*100 + int64(c.n)
}

// DivMod exercises Go's defined MinInt / -1 wrap.
func DivMod(a int64, b int64) (int64, int64) {
	return a / b, a % b
}

func DivMod32(a int32, b int32) (int32, int32) {
	return a / b, a % b
}

// BytesProbe converts a string to a fresh byte buffer and mutates
// it.
func BytesProbe(s string) int64 {
	b := []uint8(s)
	if len(b) > 0 {
		b[0] = 'X'
	}
	t := int64(0)
	for i := 0; i < len(b); i++ {
		t = t*31 + int64(b[i])
	}
	return t
}

func sliceFrom(c *Counter, n int) []int32 {
	c.n++
	out := make([]int32, n)
	for i := 0; i < n; i++ {
		out[i] = int32(i + 1)
	}
	return out
}

// RangeProbe checks that the range operand evaluates once and that
// writing the index variable cannot change the iteration.
func RangeProbe(c *Counter) int64 {
	t := int64(0)
	for i, v := range sliceFrom(c, 4) {
		t += int64(i) * int64(v)
		i = 100
	}
	return t*10 + int64(c.n)
}

// RangeValProbe checks that the value variable is a copy.
func RangeValProbe(xs []int32) int64 {
	t := int64(0)
	for _, v := range xs {
		v = v + 1
		t += int64(v)
	}
	for i := 0; i < len(xs); i++ {
		t += int64(xs[i]) * 100
	}
	return t
}

// RangeIntProbe ranges over an integer count.
func RangeIntProbe(n int) int64 {
	t := int64(0)
	for i := range n {
		t = t*2 + int64(i)
	}
	return t
}

// PartialArray checks zero fill of omitted array elements.
func PartialArray() int64 {
	a := [5]int64{7, 9}
	names := [3]string{"a"}
	t := int64(0)
	for i := 0; i < 5; i++ {
		t = t*10 + a[i]
	}
	return t*10 + int64(len(names[1])) + int64(len(names[0]))
}

// Tagged is comparable and holds a string array.
type Tagged struct {
	Tags [2]string
	N    int32
}

func TaggedEq(a Tagged, b Tagged) bool {
	return a == b
}

// OrderArgs checks left-to-right argument evaluation.
func three(a int64, b int64, x int64) int64 {
	return a*100 + b*10 + x
}

func OrderArgs(c *Counter) int64 {
	return three(bump(c, 1), bump(c, 2), bump(c, 3)) + logCode(c)
}

// OrderBinary checks left-to-right operand evaluation.
func OrderBinary(c *Counter) int64 {
	v := bump(c, 4) - 2*bump(c, 5)
	return v*10000 + logCode(c)
}

// OrderIndex checks that a base evaluates before its index.
func OrderIndex(c *Counter) int64 {
	s := sliceFrom(c, 6)
	v := int64(s[bump(c, 2)])
	return v*10000 + logCode(c)
}

// SpareProbe checks that extending a slice inside its capacity
// exposes zeroed memory, both from make and after a growing
// append.
func SpareProbe() int64 {
	s := make([]int64, 0, 4)
	s = append(s, 5)
	s = s[:4]
	t := int64(0)
	for i := 0; i < len(s); i++ {
		t = t*10 + s[i] + 1
	}
	g := make([]int64, 0, 2)
	g = append(g, 1)
	g = append(g, 2)
	g = append(g, 3)
	if cap(g) >= 4 {
		g = g[:4]
	}
	for i := 0; i < 4; i++ {
		t = t*10 + g[i] + 1
	}
	return t
}

// NilProbe checks nil-ness of zero slices against allocations.
func NilProbe() int64 {
	var s []int32
	t := int64(0)
	if s == nil {
		t++
	}
	s2 := make([]int32, 0)
	if s2 != nil {
		t += 2
	}
	s = append(s, 5)
	if s != nil {
		t += 4
	}
	return t
}

// WrapProbe exercises wrapping arithmetic at several widths.
func WrapProbe(a int64, b int64) int64 {
	return a*b + a - b
}

func Narrow32(a int32, b int32) int32 {
	return a*b - a/b + a%b
}

func WrapU8(a uint8, b uint8) uint8 {
	return (a - b) * 3
}

// AndNotProbe covers the two &^ forms.
func AndNotProbe(a uint32, b uint32) uint32 {
	a &^= b
	return a &^ (b >> 1)
}

func ShiftProbe(x uint64, n int) uint64 {
	return (x << n) >> (n / 2)
}

func ConvProbe(x int64) uint64 {
	return uint64(uint8(x)) + uint64(uint32(int32(x)))*1000
}

// SubWrite assigns through a subslice view, so the write must land
// in the shared buffer.
func SubWrite(n int) int64 {
	s := make([]int64, n)
	s[1:][0] = 7
	s[1:][1:][0] = 9
	t := int64(0)
	for i := 0; i < len(s); i++ {
		t = t*10 + s[i]
	}
	return t
}

// AndNotOrder checks that a compound &^= evaluates its place
// before its value, like every other compound assignment.
func AndNotOrder(c *Counter) int64 {
	s := make([]uint64, 8)
	s[3] = 0xFF
	s[bump(c, 1)+2] &^= uint64(bump(c, 2)) + 0x0F
	return int64(s[3])*100000 + logCode(c)
}

// ZeroArray slices a zero-length array; the view must be non-nil.
func ZeroArray() int64 {
	var a [0]int32
	t := int64(0)
	if a[:] != nil {
		t++
	}
	t = t*10 + int64(len(a[:]))
	return t
}

// MakeU64 sizes a buffer from an unsigned count.
func MakeU64(n uint64) int64 {
	s := make([]int32, n)
	return int64(len(s))
}

// PickArray indexes an array-typed call result while the index
// mutates state.
func mkTriple(x int64) [3]int64 {
	var a [3]int64
	a[0] = x
	a[1] = x + 1
	a[2] = x + 2
	return a
}

func PickArray(c *Counter) int64 {
	v := mkTriple(40)[bump(c, 2)]
	return v*10000 + logCode(c)
}
