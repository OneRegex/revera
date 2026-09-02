#include "revera.h"

#include "engine.h"

#include <limits.h>
#include <stdlib.h>
#include <string.h>

static const unsigned char locale_blob[] = {
#include "locale_data.h"
};

struct revera_locale {
    revera_eng_Locale loc;
};

struct revera_regex {
    vg_arena mem;
    revera_eng_Regexp re;
    size_t groups;
};

struct revera_iterator {
    const revera_regex *regex;
    vg_str subject;
    revera_eng_MatchIter iter;
    vg_arena hold;
    revera_eng_slice_Match matches;
};

static int64_t clamp_size(size_t n) {
    return n > (size_t)INT64_MAX ? INT64_MAX : (int64_t)n;
}

static vg_str view(const char *p, size_t n) {
    return (vg_str){p, clamp_size(n)};
}

static revera_status status_of(int32_t code) {
    switch (code) {
    case revera_eng_ErrNone:
        return REVERA_OK;
    case revera_eng_ErrBadPat:
        return REVERA_PATTERN;
    case revera_eng_ErrECollate:
        return REVERA_COLLATING_ELEMENT;
    case revera_eng_ErrECType:
        return REVERA_CHARACTER_CLASS;
    case revera_eng_ErrEEscape:
        return REVERA_ESCAPE;
    case revera_eng_ErrESubReg:
        return REVERA_BACK_REFERENCE;
    case revera_eng_ErrEBrack:
        return REVERA_BRACKET;
    case revera_eng_ErrEParen:
        return REVERA_PAREN;
    case revera_eng_ErrEBrace:
        return REVERA_BRACE;
    case revera_eng_ErrBadBR:
        return REVERA_INTERVAL;
    case revera_eng_ErrERange:
        return REVERA_RANGE;
    case revera_eng_ErrESpace:
        return REVERA_CAPACITY;
    case revera_eng_ErrBadRpt:
        return REVERA_REPEAT;
    case revera_eng_ErrENoSub:
        return REVERA_NO_CAPTURES;
    default:
        return REVERA_UNKNOWN;
    }
}

static const char *message_of(int32_t code) {
    switch (code) {
    case revera_eng_ErrNone:
        return "";
    case revera_eng_ErrNoMatch:
        return "no match";
    case revera_eng_ErrBadPat:
        return "invalid regular expression";
    case revera_eng_ErrECollate:
        return "invalid collating element";
    case revera_eng_ErrECType:
        return "invalid character class";
    case revera_eng_ErrEEscape:
        return "trailing backslash";
    case revera_eng_ErrESubReg:
        return "invalid backreference";
    case revera_eng_ErrEBrack:
        return "unclosed bracket expression";
    case revera_eng_ErrEParen:
        return "unclosed parenthesis";
    case revera_eng_ErrEBrace:
        return "unclosed interval";
    case revera_eng_ErrBadBR:
        return "invalid interval";
    case revera_eng_ErrERange:
        return "invalid range";
    case revera_eng_ErrESpace:
        return "capacity limit exceeded";
    case revera_eng_ErrBadRpt:
        return "repetition operator has no operand";
    case revera_eng_ErrENoSub:
        return "capture offsets are disabled";
    default:
        return "unknown regular expression error";
    }
}

static void clear_error(revera_error *error) {
    if (error != NULL) {
        error->status = REVERA_OK;
        error->offset = REVERA_NO_OFFSET;
        error->message = "";
    }
}

static bool set_error(revera_error *error, revera_eng_Error engine_error) {
    if (engine_error.Code == revera_eng_ErrNone) {
        clear_error(error);
        return true;
    }
    if (error != NULL) {
        error->status = status_of(engine_error.Code);
        error->offset = engine_error.Pos < 0 ? REVERA_NO_OFFSET : (size_t)engine_error.Pos;
        error->message = message_of(engine_error.Code);
    }
    return false;
}

static revera_eng_Locale locale_value(const revera_options *options) {
    if (options != NULL && options->locale != NULL) {
        return options->locale->loc;
    }
    return revera_eng_LocalePOSIX();
}

const void *revera_embedded_locale_data(size_t *size) {
    if (size != NULL) {
        *size = sizeof(locale_blob);
    }
    return locale_blob;
}

revera_locale *revera_locale_open(const char *name, size_t name_len,
                                  const char *collation_type, size_t collation_len) {
    vg_arena mem = {0};
    revera_eng_Tup_t4c6f63616c65x_bool result =
        revera_eng_LocaleOpen(&mem, (vg_str){(const char *)locale_blob, (int64_t)sizeof(locale_blob)},
                              view(name, name_len), view(collation_type, collation_len));
    vg_arena_free(&mem);
    if (!result.r1) {
        return NULL;
    }
    revera_locale *locale = (revera_locale *)malloc(sizeof(*locale));
    if (locale == NULL) {
        abort();
    }
    locale->loc = result.r0;
    return locale;
}

void revera_locale_free(revera_locale *locale) {
    free(locale);
}

static bool load_base(revera_eng_Locale *base) {
    revera_eng_Tup_t4c6f63616c65x_bool result =
        revera_eng_LocaleLoad((vg_str){(const char *)locale_blob, (int64_t)sizeof(locale_blob)});
    if (!result.r1) {
        return false;
    }
    *base = result.r0;
    return true;
}

size_t revera_locale_count(void) {
    revera_eng_Locale base;
    if (!load_base(&base)) {
        return 0;
    }
    return (size_t)revera_eng_LocaleCount(&base);
}

bool revera_locale_name(size_t index, const char **name, size_t *name_len) {
    revera_eng_Locale base;
    if (!load_base(&base)) {
        return false;
    }
    int64_t count = revera_eng_LocaleCount(&base);
    if (index >= (size_t)count) {
        return false;
    }
    vg_str result = revera_eng_LocaleName(&base, (int64_t)index);
    if (name != NULL) {
        *name = result.p;
    }
    if (name_len != NULL) {
        *name_len = (size_t)result.len;
    }
    return true;
}

revera_regex *revera_compile(const char *pattern, size_t pattern_len,
                             const revera_options *options, revera_error *error) {
    clear_error(error);
    revera_regex *regex = (revera_regex *)calloc(1, sizeof(*regex));
    if (regex == NULL) {
        abort();
    }
    uint32_t flags = 0;
    if (options != NULL) {
        if (options->case_insensitive) {
            flags |= revera_eng_FlagICase;
        }
        if (options->newline_sensitive) {
            flags |= revera_eng_FlagNewline;
        }
        if (options->no_captures) {
            flags |= revera_eng_FlagNoSub;
        }
        if (options->shortest_match) {
            flags |= revera_eng_FlagMinimal;
        }
    }
    vg_str source = vg_str_dup(&regex->mem, view(pattern, pattern_len));
    revera_eng_Tup_t526567657870x_t4572726f72x result =
        revera_eng_Compile(&regex->mem, source, locale_value(options), flags);
    if (!set_error(error, result.r1)) {
        revera_regex_free(regex);
        return NULL;
    }
    regex->re = result.r0;
    regex->groups = (size_t)revera_eng_NumSub(&regex->re) + 1;
    return regex;
}

void revera_regex_free(revera_regex *regex) {
    if (regex == NULL) {
        return;
    }
    vg_arena_free(&regex->mem);
    free(regex);
}

size_t revera_group_count(const revera_regex *regex) {
    return regex->groups;
}

static bool refuse_without_captures(const revera_regex *regex, revera_error *error) {
    if (regex->re.flags & revera_eng_FlagNoSub) {
        revera_eng_Error e = {revera_eng_ErrENoSub, -1};
        set_error(error, e);
        return false;
    }
    return true;
}

static revera_match match_of(revera_eng_Match match) {
    if (match.So < 0) {
        return (revera_match){REVERA_NO_OFFSET, REVERA_NO_OFFSET, false};
    }
    return (revera_match){(size_t)match.So, (size_t)match.Eo, true};
}

bool revera_matches(const revera_regex *regex, const char *subject, size_t subject_len,
                    revera_error *error) {
    vg_arena scratch = {0};
    revera_eng_Regexp copy = regex->re;
    revera_eng_Tup_bool_t4572726f72x result =
        revera_eng_Exec(&scratch, &copy, view(subject, subject_len),
                        (revera_eng_slice_Match){0}, 0);
    bool valid = set_error(error, result.r1);
    vg_arena_free(&scratch);
    return valid && result.r0;
}

bool revera_find(const revera_regex *regex, const char *subject, size_t subject_len,
                 revera_match *out, revera_error *error) {
    if (!refuse_without_captures(regex, error)) {
        return false;
    }
    vg_arena scratch = {0};
    revera_eng_slice_Match pmatch = revera_eng_slice_Match_make(&scratch, 1);
    revera_eng_Regexp copy = regex->re;
    revera_eng_Tup_bool_t4572726f72x result =
        revera_eng_Exec(&scratch, &copy, view(subject, subject_len), pmatch, 0);
    bool valid = set_error(error, result.r1);
    if (valid && result.r0 && out != NULL) {
        *out = match_of(pmatch.p[0]);
    }
    vg_arena_free(&scratch);
    return valid && result.r0;
}

bool revera_captures(const revera_regex *regex, const char *subject, size_t subject_len,
                     revera_match *groups, size_t group_cap, revera_error *error) {
    if (!refuse_without_captures(regex, error)) {
        return false;
    }
    if (groups == NULL || group_cap < regex->groups) {
        revera_eng_Error e = {revera_eng_ErrESpace, -1};
        set_error(error, e);
        return false;
    }
    vg_arena scratch = {0};
    revera_eng_slice_Match pmatch =
        revera_eng_slice_Match_make(&scratch, (int64_t)regex->groups);
    revera_eng_Regexp copy = regex->re;
    revera_eng_Tup_bool_t4572726f72x result =
        revera_eng_Exec(&scratch, &copy, view(subject, subject_len), pmatch, 0);
    bool valid = set_error(error, result.r1);
    if (valid && result.r0) {
        for (size_t i = 0; i < regex->groups; i++) {
            groups[i] = match_of(pmatch.p[i]);
        }
    }
    vg_arena_free(&scratch);
    return valid && result.r0;
}

revera_iterator *revera_iterator_new(const revera_regex *regex, const char *subject,
                                     size_t subject_len, int64_t limit, revera_error *error) {
    clear_error(error);
    if (!refuse_without_captures(regex, error)) {
        return NULL;
    }
    revera_eng_Regexp copy = regex->re;
    revera_eng_Tup_t4d6174636849746572x_t4572726f72x result =
        revera_eng_MatchIterInit(&copy, limit);
    if (!set_error(error, result.r1)) {
        return NULL;
    }
    revera_iterator *iterator = (revera_iterator *)calloc(1, sizeof(*iterator));
    if (iterator == NULL) {
        abort();
    }
    iterator->regex = regex;
    iterator->subject = view(subject, subject_len);
    iterator->iter = result.r0;
    iterator->matches = revera_eng_slice_Match_make(&iterator->hold, (int64_t)regex->groups);
    return iterator;
}

bool revera_iterator_next(revera_iterator *iterator, revera_match *groups,
                          size_t group_cap, revera_error *error) {
    if (groups == NULL || group_cap < iterator->regex->groups) {
        revera_eng_Error e = {revera_eng_ErrESpace, -1};
        set_error(error, e);
        return false;
    }
    vg_arena step = {0};
    revera_eng_Regexp copy = iterator->regex->re;
    revera_eng_Tup_bool_t4572726f72x result =
        revera_eng_MatchIterNext(&step, &copy, &iterator->iter, iterator->subject, 0,
                                 iterator->matches);
    bool valid = set_error(error, result.r1);
    if (valid && result.r0) {
        for (size_t i = 0; i < iterator->regex->groups; i++) {
            groups[i] = match_of(iterator->matches.p[i]);
        }
    }
    vg_arena_free(&step);
    return valid && result.r0;
}

void revera_iterator_free(revera_iterator *iterator) {
    if (iterator == NULL) {
        return;
    }
    vg_arena_free(&iterator->hold);
    free(iterator);
}

static char *replace(const revera_regex *regex, const char *subject, size_t subject_len,
                     const char *replacement, size_t replacement_len, int64_t limit,
                     size_t *out_len, revera_error *error) {
    vg_arena scratch = {0};
    revera_eng_Regexp copy = regex->re;
    revera_eng_Tup_Str_t4572726f72x result =
        revera_eng_ReplaceAll(&scratch, &copy, view(subject, subject_len),
                              view(replacement, replacement_len), limit, 0);
    if (!set_error(error, result.r1)) {
        vg_arena_free(&scratch);
        return NULL;
    }
    size_t n = (size_t)result.r0.len;
    char *out = (char *)malloc(n + 1);
    if (out == NULL) {
        abort();
    }
    if (n > 0) {
        memcpy(out, result.r0.p, n);
    }
    out[n] = '\0';
    if (out_len != NULL) {
        *out_len = n;
    }
    vg_arena_free(&scratch);
    return out;
}

char *revera_replace_all(const revera_regex *regex, const char *subject, size_t subject_len,
                         const char *replacement, size_t replacement_len,
                         size_t *out_len, revera_error *error) {
    return replace(regex, subject, subject_len, replacement, replacement_len, -1,
                   out_len, error);
}

char *revera_replace_first_n(const revera_regex *regex, const char *subject, size_t subject_len,
                             const char *replacement, size_t replacement_len, size_t limit,
                             size_t *out_len, revera_error *error) {
    return replace(regex, subject, subject_len, replacement, replacement_len,
                   clamp_size(limit), out_len, error);
}

static revera_backend_contract backend_of(revera_eng_BackendContract value) {
    return (revera_backend_contract){(uint64_t)value.HeapBytes,
                                     (uint64_t)value.StackBytes,
                                     (uint64_t)value.Steps};
}

revera_contract revera_contract_for(const revera_regex *regex, size_t max_input) {
    revera_eng_Regexp copy = regex->re;
    revera_eng_Contract value = revera_eng_ContractFor(&copy, clamp_size(max_input));
    revera_contract result = {0};
    result.max_input = (size_t)value.MaxInput;
    result.heap_bytes = (uint64_t)revera_eng_ContractHeapBytes(&value);
    result.stack_bytes = (uint64_t)revera_eng_ContractStackBytes(&value);
    result.steps = (uint64_t)revera_eng_ContractSteps(&value);
    result.matcher = backend_of(value.Matcher);
    result.has_one_pass = value.HasOnePass;
    result.one_pass = backend_of(value.OnePass);
    result.has_solver = value.HasSolver;
    result.solver = backend_of(value.Solver);
    return result;
}
