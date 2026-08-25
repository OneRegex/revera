// Minimal runtime for the generated Vego engine. It supplies what
// the Go runtime supplied implicitly: growable buffers, immutable
// string views, and memory.
//
// Memory comes from three arenas. Locale data loads into the
// persistent arena once. The pattern arena holds one compiled
// Regexp and resets at every compile. The scratch arena holds one
// operation's workspace and resets at every operation. This
// mirrors the Vego ownership model: nothing outlives the compiled
// pattern except locale data.
//
// The whole program is single threaded, as the Vego specification
// requires, so the raw statics below are safe in practice.

#![allow(dead_code)]

use std::alloc::Layout;

struct Arena {
    blocks: Vec<(*mut u8, Layout)>,
}

static mut PERSISTENT: Arena = Arena { blocks: Vec::new() };
static mut PATTERN: Arena = Arena { blocks: Vec::new() };
static mut SCRATCH: Arena = Arena { blocks: Vec::new() };
static mut MODE: u8 = 0;

pub fn use_persistent_arena() {
    unsafe { MODE = 0 };
}

pub fn use_pattern_arena() {
    unsafe { MODE = 1 };
}

pub fn use_scratch_arena() {
    unsafe { MODE = 2 };
}

fn arena_reset(a: *mut Arena) {
    unsafe {
        for (p, l) in (*a).blocks.drain(..) {
            std::alloc::dealloc(p, l);
        }
    }
}

pub fn reset_pattern_arena() {
    arena_reset(&raw mut PATTERN);
    arena_reset(&raw mut SCRATCH);
}

pub fn reset_scratch_arena() {
    arena_reset(&raw mut SCRATCH);
}

fn arena_alloc(layout: Layout) -> *mut u8 {
    unsafe {
        let a: *mut Arena = match MODE {
            0 => &raw mut PERSISTENT,
            1 => &raw mut PATTERN,
            _ => &raw mut SCRATCH,
        };
        let p = std::alloc::alloc_zeroed(layout);
        assert!(!p.is_null(), "out of memory");
        (*a).blocks.push((p, layout));
        p
    }
}

fn alloc_elems<T>(n: i64) -> *mut T {
    let count = if n < 1 { 1 } else { n as usize };
    let layout = Layout::array::<T>(count).unwrap();
    arena_alloc(layout) as *mut T
}

// Str is an immutable byte view, the translation of a Go string.
// The zero value is the empty string.
pub struct Str {
    pub p: *const u8,
    pub len: i64,
}

impl Clone for Str {
    fn clone(&self) -> Self {
        *self
    }
}

impl Copy for Str {}

unsafe impl Send for Str {}
unsafe impl Sync for Str {}

impl Str {
    pub fn byte(self, i: i64) -> u8 {
        assert!(i >= 0 && i < self.len);
        unsafe { *self.p.add(i as usize) }
    }

    pub fn sub(self, lo: i64, hi: i64) -> Str {
        assert!(0 <= lo && lo <= hi && hi <= self.len);
        if self.p.is_null() {
            return zero();
        }
        Str {
            p: unsafe { self.p.add(lo as usize) },
            len: hi - lo,
        }
    }

    pub fn tail(self, lo: i64) -> Str {
        self.sub(lo, self.len)
    }

    pub fn head(self, hi: i64) -> Str {
        self.sub(0, hi)
    }

    pub fn bytes<'a>(self) -> &'a [u8] {
        if self.p.is_null() || self.len == 0 {
            return &[];
        }
        unsafe { std::slice::from_raw_parts(self.p, self.len as usize) }
    }
}

pub const fn lit(s: &'static [u8]) -> Str {
    Str {
        p: s.as_ptr(),
        len: s.len() as i64,
    }
}

pub fn streq(a: Str, b: Str) -> bool {
    a.bytes() == b.bytes()
}

pub fn strcmp3(a: Str, b: Str) -> i32 {
    match a.bytes().cmp(b.bytes()) {
        std::cmp::Ordering::Less => -1,
        std::cmp::Ordering::Equal => 0,
        std::cmp::Ordering::Greater => 1,
    }
}

// Slice is a Go slice header: pointer, length, capacity.
// Assignment copies the header and shares the buffer, exactly like
// Go. A null pointer is the nil slice; every allocation, even a
// zero-length one, produces a non-null pointer.
pub struct Slice<T> {
    pub p: *mut T,
    pub len: i64,
    pub cap: i64,
}

impl<T> Clone for Slice<T> {
    fn clone(&self) -> Self {
        *self
    }
}

impl<T> Copy for Slice<T> {}

unsafe impl<T> Send for Slice<T> {}
unsafe impl<T> Sync for Slice<T> {}

impl<T> Slice<T> {
    // ptr addresses one element for writing. Bounds are checked
    // here, so generated writes stay as safe as Go's.
    pub fn ptr(self, i: i64) -> *mut T {
        assert!(i >= 0 && i < self.len);
        unsafe { self.p.add(i as usize) }
    }

    pub fn sub(self, lo: i64, hi: i64) -> Slice<T> {
        assert!(0 <= lo && lo <= hi && hi <= self.cap);
        if self.p.is_null() {
            return Slice { p: std::ptr::null_mut(), len: 0, cap: 0 };
        }
        Slice {
            p: unsafe { self.p.add(lo as usize) },
            len: hi - lo,
            cap: self.cap - lo,
        }
    }

    pub fn tail(self, lo: i64) -> Slice<T> {
        self.sub(lo, self.len)
    }

    pub fn head(self, hi: i64) -> Slice<T> {
        self.sub(0, hi)
    }
}

impl<T: Copy> Slice<T> {
    pub fn get(self, i: i64) -> T {
        assert!(i >= 0 && i < self.len);
        unsafe { *self.p.add(i as usize) }
    }
}

pub fn make<T>(n: i64) -> Slice<T> {
    make_cap(n, n)
}

pub fn make_cap<T>(n: i64, c: i64) -> Slice<T> {
    assert!(0 <= n && n <= c);
    Slice { p: alloc_elems(c), len: n, cap: c }
}

fn grow<T>(s: Slice<T>, need: i64) -> Slice<T> {
    let mut newcap = if s.cap * 2 > 8 { s.cap * 2 } else { 8 };
    if newcap < need {
        newcap = need;
    }
    let p = alloc_elems::<T>(newcap);
    if !s.p.is_null() && s.len > 0 {
        unsafe { std::ptr::copy_nonoverlapping(s.p, p, s.len as usize) };
    }
    Slice { p, len: s.len, cap: newcap }
}

pub fn append<T>(s: Slice<T>, v: T) -> Slice<T> {
    let mut out = s;
    if out.len == out.cap {
        out = grow(out, out.len + 1);
    }
    unsafe { std::ptr::write(out.p.add(out.len as usize), v) };
    out.len += 1;
    out
}

pub fn append_slice<T>(s: Slice<T>, more: Slice<T>) -> Slice<T> {
    let mut out = s;
    if out.len + more.len > out.cap {
        out = grow(out, out.len + more.len);
    }
    if more.len > 0 {
        // The source may alias the old buffer; the old buffer is
        // still intact after a grow, so a plain copy is right.
        unsafe { std::ptr::copy(more.p, out.p.add(out.len as usize), more.len as usize) };
    }
    out.len += more.len;
    out
}

pub fn append_str(s: Slice<u8>, more: Str) -> Slice<u8> {
    let mut out = s;
    if out.len + more.len > out.cap {
        out = grow(out, out.len + more.len);
    }
    if more.len > 0 {
        unsafe { std::ptr::copy(more.p, out.p.add(out.len as usize), more.len as usize) };
    }
    out.len += more.len;
    out
}

pub fn vcopy<T>(dst: Slice<T>, src: Slice<T>) -> i64 {
    let n = std::cmp::min(dst.len, src.len);
    if n > 0 {
        unsafe { std::ptr::copy(src.p, dst.p, n as usize) };
    }
    n
}

pub fn vcopy_str(dst: Slice<u8>, src: Str) -> i64 {
    let n = std::cmp::min(dst.len, src.len);
    if n > 0 {
        unsafe { std::ptr::copy(src.p, dst.p, n as usize) };
    }
    n
}

// bytes_from_str copies a string into a fresh mutable byte
// buffer, the []uint8(s) conversion.
pub fn bytes_from_str(s: Str) -> Slice<u8> {
    let out = make::<u8>(s.len);
    if s.len > 0 {
        unsafe {
            std::ptr::copy_nonoverlapping(s.p, out.p, s.len as usize);
        }
    }
    out
}

pub fn str_from_bytes(b: Slice<u8>) -> Str {
    let p = alloc_elems::<u8>(b.len);
    if b.len > 0 {
        unsafe { std::ptr::copy_nonoverlapping(b.p, p, b.len as usize) };
    }
    Str { p, len: b.len }
}

// arr_slice views a stack or struct array as a slice, sharing its
// storage, like Go's array slicing.
pub fn arr_slice<T, const N: usize>(p: *mut [T; N], lo: i64, hi: i64) -> Slice<T> {
    let n = N as i64;
    assert!(0 <= lo && lo <= hi && hi <= n);
    Slice {
        p: unsafe { (p as *mut T).add(lo as usize) },
        len: hi - lo,
        cap: n - lo,
    }
}

pub fn slice_of<T: Copy>(src: &[T]) -> Slice<T> {
    let out = make::<T>(src.len() as i64);
    if !src.is_empty() {
        unsafe { std::ptr::copy_nonoverlapping(src.as_ptr(), out.p, src.len()) };
    }
    out
}

// zero builds the Go zero value. Every generated type is plain
// data whose zero value is all zero bytes; a Slice or Str becomes
// nil, which is exactly Go's zero slice and empty string.
pub fn zero<T>() -> T {
    unsafe { std::mem::zeroed() }
}
