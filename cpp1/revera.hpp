// POSIX.1-2024 extended regular expressions.
//
// This header is the public surface of the revera engine.
// The engine itself, in engine.hpp, is generated from a Vego program.
// It speaks in arenas, raw views and numeric flags.
// Nothing here needs those, and this header does not include them.
// A caller who wants the execution flags of regexec() includes engine.hpp and works in namespace revera::engine.
//
//     revera::Regex re("([a-z]+)([0-9]*)");
//     if (auto caps = re.captures("__abc12__")) {
//         std::cout << (*caps)[1]->str() << "\n";  // abc
//     }
//
// Patterns and subjects are UTF-8.
// The language is the POSIX ERE language: leftmost-longest matching, no backreferences, and no Perl escapes.
// Bracket expressions read their character classes, collating elements and equivalence classes from a Locale.
// The default locale is POSIX.
//
// Compilation and search failures throw revera::Error.
// A subject that simply does not match is not a failure, and the search returns an empty optional.

#pragma once

#include <cstddef>
#include <cstdint>
#include <memory>
#include <optional>
#include <stdexcept>
#include <string>
#include <string_view>
#include <vector>

namespace revera {

// The largest interval count a pattern may ask for, as in a{0,255}.
inline constexpr int dup_max = 255;

// Returns the CLDR locale blob compiled into this library.
std::string_view embedded_locale_data();

// Every way compilation or a search can fail.
// The names follow the <regex.h> error constants.
enum class Failure {
    // The pattern is not a valid extended regular expression.
    Pattern,
    // A [[.x.]] reference names no collating element.
    CollatingElement,
    // A [[:x:]] reference names no character class.
    CharacterClass,
    // The pattern ends with a backslash.
    Escape,
    // A backreference, which the ERE language does not have.
    BackReference,
    // A bracket expression is not closed.
    Bracket,
    // A parenthesis is not closed.
    Paren,
    // An interval brace is not closed.
    Brace,
    // The interval content is not a valid count or count range.
    Interval,
    // A range like [z-a] runs backwards, or its endpoint is not a single character.
    Range,
    // The work needed passed a capacity limit.
    Capacity,
    // A repetition operator has no operand to repeat.
    Repeat,
    // The expression was compiled with no_captures, and the call needs match offsets.
    NoCaptures,
    // A code this version of the header does not name.
    Unknown,
};

// A compilation or search failure.
class Error : public std::runtime_error {
  public:
    Error(Failure failure, std::optional<size_t> offset, const std::string& what)
        : std::runtime_error(what), failure_(failure), offset_(offset) {}

    // Returns which failure this is.
    Failure failure() const noexcept { return failure_; }

    // Returns the byte offset in the pattern where compilation stopped, when the failure has one.
    std::optional<size_t> offset() const noexcept { return offset_; }

  private:
    Failure failure_;
    std::optional<size_t> offset_;
};

// One matched span of a subject.
// It borrows the subject, so it stays valid only as long as the subject does.
class Match {
  public:
    Match(std::string_view subject, size_t start, size_t end)
        : subject_(subject), start_(start), end_(end) {}

    // Returns the byte offset where the match starts.
    size_t start() const noexcept { return start_; }

    // Returns the byte offset one past the end of the match.
    size_t end() const noexcept { return end_; }

    // Returns the length of the match in bytes.
    size_t size() const noexcept { return end_ - start_; }

    // Reports whether the match is the null string.
    bool empty() const noexcept { return start_ == end_; }

    // Returns the matched text.
    std::string_view str() const { return subject_.substr(start_, end_ - start_); }

  private:
    std::string_view subject_;
    size_t start_;
    size_t end_;
};

// One match and the spans of its capturing groups.
// Element 0 is the whole match.
// A group that took no part in the match reads as std::nullopt.
using Captures = std::vector<std::optional<Match>>;

// A locale: the source of character classes, case folding, collating elements and equivalence classes.
//
// A Locale never changes and is cheap to copy.
// The default locale is POSIX.
class Locale {
  public:
    Locale();

    // Resolves a CLDR locale name against the embedded data, for example Locale::open("cs").
    // An empty collation type takes the standard collation of the locale.
    // The result is empty when the name or the collation type is unknown.
    static std::optional<Locale> open(std::string_view name,
                                      std::string_view collation_type = "");

    // Returns every locale name the embedded data carries.
    static std::vector<std::string> names();

  private:
    friend class Regex;
    struct Data;
    std::shared_ptr<const Data> data_;
};

// What one backend of one search can use.
struct BackendContract {
    // The bound on explicit heap allocation, in bytes.
    uint64_t heap_bytes;
    // The estimate of the deepest call stack, in bytes.
    uint64_t stack_bytes;
    // The bound on abstract operations.
    // These are unit-cost operations, not nanoseconds.
    uint64_t steps;
};

// What one search can cost, from Regex::contract.
//
// Every figure saturates at 1 << 62, which marks a bound too large to be useful.
struct Contract {
    // The subject length the figures cover, in bytes.
    size_t max_input;
    // The heap bound of a whole search, in bytes.
    uint64_t heap_bytes;
    // The stack estimate of a whole search, in bytes.
    uint64_t stack_bytes;
    // The step bound of a whole search.
    uint64_t steps;
    // The figures of the automaton, which every search runs.
    BackendContract matcher;
    // The figures of the one-pass capture walk, set when compilation proved that every span has one parse.
    std::optional<BackendContract> one_pass;
    // The figures of the memoized capture search, the ceiling for any search that fills group offsets.
    std::optional<BackendContract> solver;
};

// How to compile a pattern.
struct Options {
    // Matches upper and lower case alike, like REG_ICASE.
    bool case_insensitive = false;
    // Gives ^ and $ their line meaning, like REG_NEWLINE.
    // It also stops dot and negated brackets on a newline.
    bool newline_sensitive = false;
    // Compiles for a yes-or-no answer only, like REG_NOSUB.
    // Regex::matches still works, and every other search throws with Failure::NoCaptures.
    bool no_captures = false;
    // Makes every duplication prefer the shortest repetition.
    // A repetition modifier reverses one duplication back.
    bool shortest_match = false;
    // The locale the bracket expressions read.
    Locale locale = {};
};

// A compiled regular expression.
//
// A search is const and keeps no state between calls.
// One Regex therefore serves any number of threads.
class Regex {
  public:
    // Compiles a pattern, or throws Error.
    explicit Regex(std::string_view pattern, const Options& options = {});

    Regex(Regex&&) noexcept;
    Regex& operator=(Regex&&) noexcept;
    ~Regex();

    // Returns the number of groups a search reports, counting the whole match.
    // It is one more than the number of parenthesized subexpressions.
    size_t group_count() const noexcept;

    // Reports whether the expression matches anywhere in subject.
    bool matches(std::string_view subject) const;

    // Returns the leftmost-longest match, if there is one.
    std::optional<Match> find(std::string_view subject) const;

    // Returns the leftmost-longest match with its groups, if there is one.
    std::optional<Captures> captures(std::string_view subject) const;

    // Returns every non-overlapping match, left to right.
    std::vector<Match> find_all(std::string_view subject) const;

    // Returns every non-overlapping match with its groups, left to right.
    std::vector<Captures> capture_all(std::string_view subject) const;

    // Returns subject with every non-overlapping match replaced, like the sed command s///g.
    //
    // In replacement, & stands for the whole match and \1 through \9 for one group.
    // A backslash escapes the next character, so \& and \\ are literal.
    std::string replace_all(std::string_view subject, std::string_view replacement) const;

    // Returns subject with at most limit matches replaced.
    // The rest of the subject stays as it is.
    std::string replace_first_n(std::string_view subject, std::string_view replacement,
                                size_t limit) const;

    // Returns what one search can cost on a subject of at most max_input bytes.
    // An application compares the figures against its budget.
    // It can then refuse the expression before the expression ever runs.
    Contract contract(size_t max_input) const;

  private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};

} // namespace revera
