// POSIX.1-2024 extended regular expressions.
//
// This header is the public C11 surface of the revera engine.
// The engine itself is generated from a Vego program and stays behind opaque handles.
// Patterns and subjects are byte strings that contain UTF-8.
// All offsets are byte offsets.
//
// The language is the POSIX ERE language with leftmost-longest matching.
// It has no backreferences or Perl escapes.
// The default locale is POSIX.

#ifndef REVERA_H
#define REVERA_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define REVERA_DUP_MAX 255
#define REVERA_NO_OFFSET ((size_t)-1)

typedef struct revera_regex revera_regex;
typedef struct revera_locale revera_locale;
typedef struct revera_iterator revera_iterator;

// revera_status names every way compilation or a search can fail.
typedef enum {
    REVERA_OK = 0,
    REVERA_PATTERN,
    REVERA_COLLATING_ELEMENT,
    REVERA_CHARACTER_CLASS,
    REVERA_ESCAPE,
    REVERA_BACK_REFERENCE,
    REVERA_BRACKET,
    REVERA_PAREN,
    REVERA_BRACE,
    REVERA_INTERVAL,
    REVERA_RANGE,
    REVERA_CAPACITY,
    REVERA_REPEAT,
    REVERA_NO_CAPTURES,
    REVERA_UNKNOWN
} revera_status;

// revera_error reports one failed operation.
// offset is REVERA_NO_OFFSET when the failure has no pattern position.
// message points to static storage.
typedef struct {
    revera_status status;
    size_t offset;
    const char *message;
} revera_error;

// revera_match holds one matched span of a subject.
// participated is false for a group that took no part in the match.
typedef struct {
    size_t start;
    size_t end;
    bool participated;
} revera_match;

// revera_options controls compilation.
// Its zero value selects the default behavior and the POSIX locale.
typedef struct {
    bool case_insensitive;
    bool newline_sensitive;
    bool no_captures;
    bool shortest_match;
    const revera_locale *locale;
} revera_options;

typedef struct {
    uint64_t heap_bytes;
    uint64_t stack_bytes;
    uint64_t steps;
} revera_backend_contract;

typedef struct {
    size_t max_input;
    uint64_t heap_bytes;
    uint64_t stack_bytes;
    uint64_t steps;
    revera_backend_contract matcher;
    bool has_one_pass;
    revera_backend_contract one_pass;
    bool has_solver;
    revera_backend_contract solver;
} revera_contract;

// revera_embedded_locale_data returns the CLDR locale blob compiled into this library.
const void *revera_embedded_locale_data(size_t *size);

// revera_locale_open resolves one CLDR locale name.
// An empty collation type selects the standard collation.
// The result is NULL when the name or collation type is unknown.
revera_locale *revera_locale_open(const char *name, size_t name_len,
                                  const char *collation_type, size_t collation_len);

// revera_locale_free destroys a locale returned by revera_locale_open.
void revera_locale_free(revera_locale *locale);

// revera_locale_count returns the number of locale names in the embedded data.
size_t revera_locale_count(void);

// revera_locale_name returns one borrowed locale name.
// The name points into the embedded data and stays valid for the life of the process.
bool revera_locale_name(size_t index, const char **name, size_t *name_len);

// revera_compile compiles a pattern.
// The result owns a copy of the pattern and must be released with revera_regex_free.
// It returns NULL and fills error when compilation fails.
revera_regex *revera_compile(const char *pattern, size_t pattern_len,
                             const revera_options *options, revera_error *error);

// revera_regex_free destroys a compiled expression.
void revera_regex_free(revera_regex *regex);

// revera_group_count returns the whole match plus every parenthesized group.
size_t revera_group_count(const revera_regex *regex);

// revera_matches reports whether the expression matches anywhere in subject.
// An ordinary no-match result is false with REVERA_OK in error.
bool revera_matches(const revera_regex *regex, const char *subject, size_t subject_len,
                    revera_error *error);

// revera_find returns the leftmost-longest match in out.
// It returns false with REVERA_OK when there is no match.
bool revera_find(const revera_regex *regex, const char *subject, size_t subject_len,
                 revera_match *out, revera_error *error);

// revera_captures returns the first match and its groups.
// The output array must have at least revera_group_count(regex) entries.
bool revera_captures(const revera_regex *regex, const char *subject, size_t subject_len,
                     revera_match *groups, size_t group_cap, revera_error *error);

// revera_iterator_new starts a non-overlapping left-to-right match scan.
// The iterator borrows the regular expression and the subject until revera_iterator_free.
// limit is negative for no limit.
revera_iterator *revera_iterator_new(const revera_regex *regex, const char *subject,
                                     size_t subject_len, int64_t limit, revera_error *error);

// revera_iterator_next writes one row of captures.
// It returns false with REVERA_OK when the scan is complete.
bool revera_iterator_next(revera_iterator *iterator, revera_match *groups,
                          size_t group_cap, revera_error *error);

// revera_iterator_free destroys an iterator.
void revera_iterator_free(revera_iterator *iterator);

// revera_replace_all replaces every non-overlapping match.
// The returned buffer has one trailing zero byte, which is not part of out_len.
// The caller frees it with free().
char *revera_replace_all(const revera_regex *regex, const char *subject, size_t subject_len,
                         const char *replacement, size_t replacement_len,
                         size_t *out_len, revera_error *error);

// revera_replace_first_n replaces at most limit matches.
char *revera_replace_first_n(const revera_regex *regex, const char *subject, size_t subject_len,
                             const char *replacement, size_t replacement_len, size_t limit,
                             size_t *out_len, revera_error *error);

// revera_contract_for reports the resource bounds of one search.
revera_contract revera_contract_for(const revera_regex *regex, size_t max_input);

#endif
