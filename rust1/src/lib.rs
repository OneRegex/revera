//! POSIX.1-2024 extended regular expressions.
//!
//! This crate is the Rust instantiation of the revera engine.
//! The engine itself is generated from a Vego program.
//! This file is the public surface over it.
//!
//! ```
//! # fn main() -> Result<(), revera::Error> {
//! let re = revera::Regex::new("([a-z]+)([0-9]*)")?;
//! let caps = re.captures("__abc12__")?.expect("a match");
//! assert_eq!(&caps[0], "abc12");
//! assert_eq!(&caps[1], "abc");
//! # Ok(())
//! # }
//! ```
//!
//! Patterns and subjects are UTF-8.
//! The language is the POSIX ERE language: leftmost-longest matching, no backreferences, and no Perl escapes.
//! Bracket expressions read their character classes, collating elements, and equivalence classes from a [`Locale`].
//! The default locale is POSIX.
//!
//! Every search returns a [`Result`], because a subject can exceed what the engine has capacity for.
//! [`Regex::contract`] reports that capacity ahead of time.
//!
//! The generated engine and raw-pointer runtime are private implementation details.
//!
//! ```compile_fail
//! let _ = revera::vg::Arena::new();
//! ```

// The generated engine and its runtime.
// They are the low level: explicit arenas, raw pointers, and numeric flags.
mod engine;
mod vg;

use std::fmt;
use std::ops::{Index, Range};

/// The result of any operation that the engine can refuse.
pub type Result<T> = std::result::Result<T, Error>;

static LOCALE_DATA: &[u8] = include_bytes!("data.bin");

/// Returns the CLDR locale blob compiled into this crate.
pub const fn embedded_locale_data() -> &'static [u8] {
    LOCALE_DATA
}

/// The largest interval count a pattern may ask for, as in `a{0,255}`.
pub const DUP_MAX: u32 = engine::dupMax as u32;

// The engine counts in i64.
// A usize past that range cannot be a real bound, so clamp saturates it instead of wrapping.
fn clamp(n: usize) -> i64 {
    n.min(i64::MAX as usize) as i64
}

/// What went wrong.
///
/// The variants follow the `<regex.h>` error constants.
/// `Unknown` covers a code this version of the crate does not name.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum ErrorKind {
    /// The pattern is not a valid extended regular expression.
    Pattern,
    /// A `[[.x.]]` reference names no collating element.
    CollatingElement,
    /// A `[[:x:]]` reference names no character class.
    CharacterClass,
    /// The pattern ends with a backslash.
    Escape,
    /// A backreference, which the ERE language does not have.
    BackReference,
    /// A bracket expression is not closed.
    Bracket,
    /// A parenthesis is not closed.
    Paren,
    /// An interval brace is not closed.
    Brace,
    /// The interval content is not a valid count or count range.
    Interval,
    /// A range like `[z-a]` runs backwards, or its endpoint is not a single character.
    Range,
    /// The work needed passed a capacity limit.
    Capacity,
    /// A repetition operator has no operand to repeat.
    Repeat,
    /// The expression was built with `no_captures`, and the call needs match offsets.
    NoCaptures,
    /// A code this version does not name.
    Unknown,
}

impl ErrorKind {
    fn from_code(code: i32) -> ErrorKind {
        match code {
            engine::ErrBadPat => ErrorKind::Pattern,
            engine::ErrECollate => ErrorKind::CollatingElement,
            engine::ErrECType => ErrorKind::CharacterClass,
            engine::ErrEEscape => ErrorKind::Escape,
            engine::ErrESubReg => ErrorKind::BackReference,
            engine::ErrEBrack => ErrorKind::Bracket,
            engine::ErrEParen => ErrorKind::Paren,
            engine::ErrEBrace => ErrorKind::Brace,
            engine::ErrBadBR => ErrorKind::Interval,
            engine::ErrERange => ErrorKind::Range,
            engine::ErrESpace => ErrorKind::Capacity,
            engine::ErrBadRpt => ErrorKind::Repeat,
            engine::ErrENoSub => ErrorKind::NoCaptures,
            _ => ErrorKind::Unknown,
        }
    }
}

/// A compilation or search failure.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct Error {
    code: i32,
    offset: Option<usize>,
}

impl Error {
    fn new(e: engine::Error) -> Error {
        Error {
            code: e.Code,
            offset: if e.Pos < 0 {
                None
            } else {
                Some(e.Pos as usize)
            },
        }
    }

    /// Returns which failure this is.
    pub fn kind(&self) -> ErrorKind {
        ErrorKind::from_code(self.code)
    }

    /// Returns the byte offset in the pattern where compilation stopped, when the failure has one.
    pub fn offset(&self) -> Option<usize> {
        self.offset
    }

    fn message(&self) -> &'static str {
        match self.code {
            engine::ErrNone => "success",
            engine::ErrNoMatch => "no match",
            engine::ErrBadPat => "invalid regular expression",
            engine::ErrECollate => "invalid collating element",
            engine::ErrECType => "invalid character class",
            engine::ErrEEscape => "invalid or trailing backslash",
            engine::ErrESubReg => "invalid backreference",
            engine::ErrEBrack => "unbalanced bracket",
            engine::ErrEParen => "unbalanced parenthesis",
            engine::ErrEBrace => "unbalanced brace",
            engine::ErrBadBR => "invalid interval",
            engine::ErrERange => "invalid range endpoint",
            engine::ErrESpace => "capacity limit reached",
            engine::ErrBadRpt => "repetition without an operand",
            engine::ErrENoSub => "offsets requested from a NoSub expression",
            _ => "unknown error",
        }
    }
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.offset {
            Some(at) => write!(f, "{} at byte {}", self.message(), at),
            None => f.write_str(self.message()),
        }
    }
}

impl std::error::Error for Error {}

/// One matched span of a subject.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct Match<'t> {
    text: &'t str,
    start: usize,
    end: usize,
}

impl<'t> Match<'t> {
    /// Returns the byte offset where the match starts.
    pub fn start(&self) -> usize {
        self.start
    }

    /// Returns the byte offset one past the end of the match.
    pub fn end(&self) -> usize {
        self.end
    }

    /// Returns the match as a byte range of the subject.
    pub fn range(&self) -> Range<usize> {
        self.start..self.end
    }

    /// Returns the length of the match in bytes.
    pub fn len(&self) -> usize {
        self.end - self.start
    }

    /// Reports whether the match is the null string.
    pub fn is_empty(&self) -> bool {
        self.start == self.end
    }

    /// Returns the matched text.
    pub fn as_str(&self) -> &'t str {
        &self.text[self.start..self.end]
    }
}

impl<'t> From<Match<'t>> for Range<usize> {
    fn from(m: Match<'t>) -> Range<usize> {
        m.range()
    }
}

/// One match and the spans of its capturing groups.
///
/// Group 0 is the whole match.
/// A group that took no part in the match reads as `None`.
/// Indexing such a group panics, so use [`Captures::get`] when a group is optional.
#[derive(Clone, Debug)]
pub struct Captures<'t> {
    text: &'t str,
    spans: Box<[(i64, i64)]>,
}

#[allow(clippy::len_without_is_empty)]
impl<'t> Captures<'t> {
    fn new(text: &'t str, spans: Vec<(i64, i64)>) -> Captures<'t> {
        Captures {
            text,
            spans: spans.into_boxed_slice(),
        }
    }

    /// Returns group `i`, or `None` when it took no part in the match or does not exist.
    pub fn get(&self, i: usize) -> Option<Match<'t>> {
        let (so, eo) = *self.spans.get(i)?;
        if so < 0 {
            return None;
        }
        Some(Match {
            text: self.text,
            start: so as usize,
            end: eo as usize,
        })
    }

    /// Returns the number of groups, counting the whole match.
    pub fn len(&self) -> usize {
        self.spans.len()
    }

    /// Returns every group in order, starting with the whole match.
    pub fn iter(&self) -> impl Iterator<Item = Option<Match<'t>>> + '_ {
        (0..self.len()).map(|i| self.get(i))
    }
}

impl<'t> Index<usize> for Captures<'t> {
    type Output = str;

    fn index(&self, i: usize) -> &str {
        self.get(i)
            .map(|m| m.as_str())
            .unwrap_or_else(|| panic!("group {i} did not participate in the match"))
    }
}

/// A locale: the source of character classes, case folding, collating elements, and equivalence classes.
#[derive(Clone, Copy)]
pub struct Locale {
    inner: engine::Locale,
}

// A Locale reads the blob compiled into this crate.
// That blob never changes and lives for the whole program.
unsafe impl Send for Locale {}
unsafe impl Sync for Locale {}

impl Locale {
    /// Returns the POSIX locale, also called the C locale.
    pub fn posix() -> Locale {
        Locale {
            inner: engine::LocalePOSIX(),
        }
    }

    /// Resolves a CLDR locale name against the embedded data, for example `Locale::open("cs", "")`.
    /// An empty collation type takes the standard collation of the locale.
    /// The result is `None` when the name or the collation type is unknown.
    pub fn open(name: &str, collation_type: &str) -> Option<Locale> {
        let mem = vg::Arena::new();
        let (inner, ok) = engine::LocaleOpen(
            &mem,
            vg::lit(LOCALE_DATA),
            vg::view(name.as_bytes()),
            vg::view(collation_type.as_bytes()),
        );
        if !ok {
            return None;
        }
        Some(Locale { inner })
    }

    /// Returns every locale name the embedded data carries.
    pub fn names() -> Vec<String> {
        let (mut base, ok) = engine::LocaleLoad(vg::lit(LOCALE_DATA));
        if !ok {
            return Vec::new();
        }
        (0..engine::LocaleCount(&mut base))
            .map(|i| String::from_utf8_lossy(engine::LocaleName(&mut base, i).bytes()).into_owned())
            .collect()
    }
}

impl Default for Locale {
    fn default() -> Locale {
        Locale::posix()
    }
}

impl fmt::Debug for Locale {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Locale")
    }
}

/// A compiled regular expression.
///
/// A search takes `&self` and keeps no state between calls.
/// One `Regex` therefore serves any number of threads.
pub struct Regex {
    // The arena owns the compiled program.
    // build() writes it once, and every later call only reads it.
    // Nothing reads the field itself.
    // It is here to free the program with the Regex.
    #[allow(dead_code)]
    mem: vg::Arena,
    re: engine::Regexp,
    groups: usize,
}

// The compiled program never changes after build().
// Every search copies the header it walks.
// Every allocation a search makes goes to an arena that the search owns.
unsafe impl Send for Regex {}
unsafe impl Sync for Regex {}

impl Regex {
    /// Compiles `pattern` in the POSIX locale with no options.
    pub fn new(pattern: &str) -> Result<Regex> {
        RegexBuilder::new(pattern).build()
    }

    /// Returns the number of groups a search reports, counting the whole match.
    /// It is one more than the number of parenthesized subexpressions.
    pub fn captures_len(&self) -> usize {
        self.groups
    }

    /// Reports whether the expression matches anywhere in `subject`.
    pub fn is_match(&self, subject: &str) -> Result<bool> {
        Ok(self.exec(subject, 0, |_| ())?.is_some())
    }

    /// Returns the leftmost-longest match, if there is one.
    pub fn find<'t>(&self, subject: &'t str) -> Result<Option<Match<'t>>> {
        self.refuse_without_captures()?;
        self.exec(subject, 1, |pmatch| {
            let whole = pmatch.get(0);
            Match {
                text: subject,
                start: whole.So as usize,
                end: whole.Eo as usize,
            }
        })
    }

    /// Returns the leftmost-longest match with its groups, if there is one.
    pub fn captures<'t>(&self, subject: &'t str) -> Result<Option<Captures<'t>>> {
        self.refuse_without_captures()?;
        self.exec(subject, self.groups, |pmatch| {
            Captures::new(subject, spans_of(pmatch))
        })
    }

    /// Returns every non-overlapping match, left to right.
    /// An expression built with `no_captures` reports [`ErrorKind::NoCaptures`] from the first `next`.
    pub fn find_iter<'r, 't>(&'r self, subject: &'t str) -> Matches<'r, 't> {
        Matches {
            inner: Scan::new(self, subject),
        }
    }

    /// Returns every non-overlapping match with its groups, left to right.
    /// An expression built with `no_captures` reports [`ErrorKind::NoCaptures`] from the first `next`.
    pub fn captures_iter<'r, 't>(&'r self, subject: &'t str) -> CaptureMatches<'r, 't> {
        CaptureMatches {
            inner: Scan::new(self, subject),
        }
    }

    /// Returns `subject` with every non-overlapping match replaced, like the `sed` command `s///g`.
    ///
    /// In `replacement`, `&` stands for the whole match and `\1` through `\9` for one group.
    /// A backslash escapes the next character, so `\&` and `\\` are literal.
    pub fn replace_all(&self, subject: &str, replacement: &str) -> Result<String> {
        self.replace_bounded(subject, replacement, -1)
    }

    /// Returns `subject` with at most `limit` matches replaced.
    /// The rest of the subject stays as it is.
    pub fn replacen(&self, subject: &str, limit: usize, replacement: &str) -> Result<String> {
        self.replace_bounded(subject, replacement, clamp(limit))
    }

    /// Returns `subject` with every non-overlapping match replaced by what `repl` returns for it.
    pub fn replace_all_with(
        &self,
        subject: &str,
        mut repl: impl FnMut(&Captures<'_>) -> String,
    ) -> Result<String> {
        let mut out = String::new();
        let mut last = 0;
        let mut any = false;
        for caps in self.captures_iter(subject) {
            let caps = caps?;
            let whole = caps.get(0).expect("group 0 always participates");
            if !any {
                out.reserve(subject.len() + subject.len() / 8);
                any = true;
            }
            out.push_str(&subject[last..whole.start()]);
            out.push_str(&repl(&caps));
            last = whole.end();
        }
        if !any {
            return Ok(subject.to_string());
        }
        out.push_str(&subject[last..]);
        Ok(out)
    }

    /// Returns what one search can cost on a subject of at most `max_input` bytes.
    /// An application compares the figures against its budget.
    /// It can then refuse the expression before the expression ever runs.
    pub fn contract(&self, max_input: usize) -> Contract {
        let mut re = self.re;
        let mut c = engine::ContractFor(&mut re, clamp(max_input));
        Contract {
            max_input: c.MaxInput as usize,
            heap_bytes: engine::ContractHeapBytes(&mut c) as u64,
            stack_bytes: engine::ContractStackBytes(&mut c) as u64,
            steps: engine::ContractSteps(&mut c) as u64,
            matcher: backend(c.Matcher),
            one_pass: c.HasOnePass.then(|| backend(c.OnePass)),
            solver: c.HasSolver.then(|| backend(c.Solver)),
        }
    }

    fn refuse_without_captures(&self) -> Result<()> {
        if self.re.flags & engine::FlagNoSub != 0 {
            return Err(Error {
                code: engine::ErrENoSub,
                offset: None,
            });
        }
        Ok(())
    }

    // exec runs one search in an arena of its own.
    // groups is the number of offsets to fill.
    // Zero asks for existence only.
    // On a match, exec calls take before the arena goes.
    fn exec<T>(
        &self,
        subject: &str,
        groups: usize,
        take: impl FnOnce(vg::Slice<engine::Match>) -> T,
    ) -> Result<Option<T>> {
        let mem = vg::Arena::new();
        let mut re = self.re;
        let pmatch = if groups > 0 {
            vg::make::<engine::Match>(&mem, groups as i64)
        } else {
            vg::zero()
        };
        let (matched, err) = engine::Exec(&mem, &mut re, vg::view(subject.as_bytes()), pmatch, 0);
        if err.Code != engine::ErrNone {
            return Err(Error::new(err));
        }
        if !matched {
            return Ok(None);
        }
        Ok(Some(take(pmatch)))
    }

    fn replace_bounded(&self, subject: &str, replacement: &str, limit: i64) -> Result<String> {
        let mem = vg::Arena::new();
        let mut re = self.re;
        let (out, err) = engine::ReplaceAll(
            &mem,
            &mut re,
            vg::view(subject.as_bytes()),
            vg::view(replacement.as_bytes()),
            limit,
            0,
        );
        if err.Code != engine::ErrNone {
            return Err(Error::new(err));
        }
        Ok(String::from_utf8_lossy(out.bytes()).into_owned())
    }
}

impl fmt::Debug for Regex {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("Regex")
            .field("captures_len", &self.groups)
            .finish()
    }
}

fn spans_of(pmatch: vg::Slice<engine::Match>) -> Vec<(i64, i64)> {
    (0..pmatch.len)
        .map(|i| {
            let m = pmatch.get(i);
            (m.So, m.Eo)
        })
        .collect()
}

/// Builds a [`Regex`] with options.
///
/// ```
/// # fn main() -> Result<(), revera::Error> {
/// let re = revera::RegexBuilder::new("ab+")
///     .case_insensitive(true)
///     .build()?;
/// assert!(re.is_match("ABBB")?);
/// # Ok(())
/// # }
/// ```
#[derive(Clone, Debug)]
pub struct RegexBuilder<'p> {
    pattern: &'p str,
    flags: u32,
    locale: Locale,
}

impl<'p> RegexBuilder<'p> {
    /// Starts a builder for `pattern`, in the POSIX locale with no options.
    pub fn new(pattern: &'p str) -> RegexBuilder<'p> {
        RegexBuilder {
            pattern,
            flags: 0,
            locale: Locale::posix(),
        }
    }

    /// Matches upper and lower case alike, like `REG_ICASE`.
    pub fn case_insensitive(self, yes: bool) -> RegexBuilder<'p> {
        self.flag(engine::FlagICase, yes)
    }

    /// Gives `^` and `$` their line meaning, like `REG_NEWLINE`.
    /// It also stops dot and negated brackets on a newline.
    pub fn newline_sensitive(self, yes: bool) -> RegexBuilder<'p> {
        self.flag(engine::FlagNewline, yes)
    }

    /// Compiles for a yes-or-no answer only, like `REG_NOSUB`.
    /// [`Regex::is_match`] still works.
    /// Every other search reports [`ErrorKind::NoCaptures`].
    pub fn no_captures(self, yes: bool) -> RegexBuilder<'p> {
        self.flag(engine::FlagNoSub, yes)
    }

    /// Makes every duplication prefer the shortest repetition.
    /// A repetition modifier reverses one duplication back.
    pub fn shortest_match(self, yes: bool) -> RegexBuilder<'p> {
        self.flag(engine::FlagMinimal, yes)
    }

    /// Compiles in `locale` instead of the POSIX locale.
    pub fn locale(mut self, locale: Locale) -> RegexBuilder<'p> {
        self.locale = locale;
        self
    }

    /// Compiles the pattern.
    pub fn build(&self) -> Result<Regex> {
        let mem = vg::Arena::new();
        // The pattern goes into the arena first, so the caller may build one, compile it and drop it.
        let pattern = vg::str_dup(&mem, vg::view(self.pattern.as_bytes()));
        let (mut re, err) = engine::Compile(&mem, pattern, self.locale.inner, self.flags);
        if err.Code != engine::ErrNone {
            return Err(Error::new(err));
        }
        let groups = engine::NumSub(&mut re) as usize + 1;
        Ok(Regex { mem, re, groups })
    }

    fn flag(mut self, bit: u32, yes: bool) -> RegexBuilder<'p> {
        if yes {
            self.flags |= bit;
        } else {
            self.flags &= !bit;
        }
        self
    }
}

// Scan drives one iteration over the non-overlapping matches.
// Both public iterators wrap it.
struct Scan<'r, 't> {
    re: &'r Regex,
    text: &'t str,
    state: State,
}

// State names what the scan can do next.
// The walk carries the engine's iteration cursor and the header copy it walks.
// One copy of the header therefore serves every step.
// That copy is what makes the variants uneven.
// Boxing it would trade the imbalance for an allocation the scan does not need.
#[allow(clippy::large_enum_variant)]
enum State {
    Walking(engine::Regexp, engine::MatchIter),
    Failed(Error),
    Done,
}

impl<'r, 't> Scan<'r, 't> {
    fn new(re: &'r Regex, text: &'t str) -> Scan<'r, 't> {
        let mut walk = re.re;
        let (it, err) = engine::MatchIterInit(&mut walk, -1);
        let state = if err.Code == engine::ErrNone {
            State::Walking(walk, it)
        } else {
            State::Failed(Error::new(err))
        };
        Scan { re, text, state }
    }

    // step runs one iteration and hands the offsets to take.
    // take reads them before the arena goes.
    fn step<T>(&mut self, take: impl FnOnce(vg::Slice<engine::Match>) -> T) -> Option<Result<T>> {
        if let State::Failed(err) = &self.state {
            let err = *err;
            self.state = State::Done;
            return Some(Err(err));
        }
        let State::Walking(walk, it) = &mut self.state else {
            return None;
        };
        let mem = vg::Arena::new();
        let pmatch = vg::make::<engine::Match>(&mem, self.re.groups as i64);
        let (ok, err) =
            engine::MatchIterNext(&mem, walk, it, vg::view(self.text.as_bytes()), 0, pmatch);
        if err.Code != engine::ErrNone {
            self.state = State::Done;
            return Some(Err(Error::new(err)));
        }
        if !ok {
            self.state = State::Done;
            return None;
        }
        Some(Ok(take(pmatch)))
    }
}

/// The non-overlapping matches of one search, from [`Regex::find_iter`].
pub struct Matches<'r, 't> {
    inner: Scan<'r, 't>,
}

impl<'r, 't> Iterator for Matches<'r, 't> {
    type Item = Result<Match<'t>>;

    fn next(&mut self) -> Option<Result<Match<'t>>> {
        let text = self.inner.text;
        self.inner.step(|pmatch| {
            let whole = pmatch.get(0);
            Match {
                text,
                start: whole.So as usize,
                end: whole.Eo as usize,
            }
        })
    }
}

/// The non-overlapping matches of one search with their groups, from [`Regex::captures_iter`].
pub struct CaptureMatches<'r, 't> {
    inner: Scan<'r, 't>,
}

impl<'r, 't> Iterator for CaptureMatches<'r, 't> {
    type Item = Result<Captures<'t>>;

    fn next(&mut self) -> Option<Result<Captures<'t>>> {
        let text = self.inner.text;
        self.inner
            .step(|pmatch| Captures::new(text, spans_of(pmatch)))
    }
}

/// What one backend of one search can use.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct BackendContract {
    /// The bound on explicit heap allocation, in bytes.
    pub heap_bytes: u64,
    /// The estimate of the deepest call stack, in bytes.
    pub stack_bytes: u64,
    /// The bound on abstract operations.
    /// These are unit-cost operations, not nanoseconds.
    pub steps: u64,
}

/// What one search can cost, from [`Regex::contract`].
///
/// Every figure saturates at `1 << 62`, which marks a bound too large to be useful.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct Contract {
    /// The subject length the figures cover, in bytes.
    pub max_input: usize,
    /// The heap bound of a whole search, in bytes.
    pub heap_bytes: u64,
    /// The stack estimate of a whole search, in bytes.
    pub stack_bytes: u64,
    /// The step bound of a whole search.
    pub steps: u64,
    /// The figures of the automaton, which every search runs.
    pub matcher: BackendContract,
    /// The figures of the one-pass capture walk, set when compilation proved that every span has one parse.
    pub one_pass: Option<BackendContract>,
    /// The figures of the memoized capture search, the ceiling for any search that fills group offsets.
    pub solver: Option<BackendContract>,
}

fn backend(b: engine::BackendContract) -> BackendContract {
    BackendContract {
        heap_bytes: b.HeapBytes as u64,
        stack_bytes: b.StackBytes as u64,
        steps: b.Steps as u64,
    }
}
