#include "rv_locale.h"

#include <limits.h>
#include <string.h>

enum {
    RV_FIRST_SEQUENCE_ID = 0x110000,
    RV_POSIX_LOCALE_INDEX = UINT16_MAX,
    RV_POSIX_COLLATION_PROFILE = UINT16_MAX,
    RV_NORMALIZED_NAME_MAX = 127
};

typedef struct rv_case_map {
    uint32_t code_point;
    uint32_t upper;
    uint32_t lower;
} rv_case_map;

typedef struct rv_sequence_row {
    uint32_t offset;
    uint16_t length;
} rv_sequence_row;

typedef struct rv_pair {
    uint32_t element;
    uint32_t representative;
} rv_pair;

typedef struct rv_collation_profile {
    uint32_t override_first;
    uint32_t override_count;
    uint32_t add_first;
    uint32_t add_count;
    uint32_t remove_first;
    uint32_t remove_count;
} rv_collation_profile;

typedef struct rv_locale_row {
    uint32_t name_id;
    uint32_t type_first;
    uint16_t type_count;
    uint8_t case_profile;
    uint16_t default_collation;
} rv_locale_row;

typedef struct rv_type_row {
    uint16_t type_id;
    uint16_t collation_profile;
} rv_type_row;

#include "rv_locale_data.inc"

static bool
rv_valid_scalar(uint32_t code_point)
{
    return code_point <= 0x10ffff
        && !(code_point >= 0xd800 && code_point <= 0xdfff);
}

static unsigned char
rv_ascii_lower(unsigned char character)
{
    if (character >= 'A' && character <= 'Z') {
        return (unsigned char) (character + ('a' - 'A'));
    }
    return character;
}

static bool
rv_normalize_name(const char *input, char output[RV_NORMALIZED_NAME_MAX + 1])
{
    size_t length = 0;
    const unsigned char *cursor = (const unsigned char *) input;

    if (input == NULL || *input == '\0') return false;
    while (*cursor != '\0' && *cursor != '.' && *cursor != '@') {
        unsigned char character = *cursor++;
        if (character >= 0x80 || length == RV_NORMALIZED_NAME_MAX) return false;
        output[length++] = character == '_' ? '-' : (char) rv_ascii_lower(character);
    }
    output[length] = '\0';

    if (*cursor == '.') {
        char codeset[6];
        size_t codeset_length = 0;
        cursor++;
        while (*cursor != '\0' && *cursor != '@') {
            unsigned char character = rv_ascii_lower(*cursor++);
            if (character == '-') continue;
            if (codeset_length == sizeof(codeset) - 1 || character >= 0x80) return false;
            codeset[codeset_length++] = (char) character;
        }
        codeset[codeset_length] = '\0';
        if (strcmp(codeset, "utf8") != 0) return false;
    }
    return *cursor == '\0' || *cursor == '@';
}

static const char *
rv_embedded_modifier(const char *name)
{
    const char *modifier = strchr(name, '@');
    static const char keyword[] = "collation=";
    size_t i;
    if (modifier == NULL) return NULL;
    modifier++;
    for (i = 0; i < sizeof(keyword) - 1; i++) {
        if (modifier[i] == '\0') return NULL;
        if (rv_ascii_lower((unsigned char) modifier[i]) !=
                (unsigned char) keyword[i]) return NULL;
    }
    return modifier[i] == '\0' ? NULL : modifier + i;
}

static int
rv_compare_name(const char *name, const char *pool, const uint32_t *offsets,
                uint32_t index)
{
    return strcmp(name, pool + offsets[index]);
}

static int
rv_find_name(const char *name, const char *pool, const uint32_t *offsets,
             uint32_t count)
{
    uint32_t low = 0;
    uint32_t high = count;
    while (low < high) {
        uint32_t middle = low + (high - low) / 2;
        int comparison = rv_compare_name(name, pool, offsets, middle);
        if (comparison == 0) return (int) middle;
        if (comparison < 0) high = middle;
        else low = middle + 1;
    }
    return -1;
}

static const char *
rv_long_type_alias(const char *type)
{
    if (strcmp(type, "dict") == 0) return "dictionary";
    if (strcmp(type, "phonebk") == 0) return "phonebook";
    if (strcmp(type, "trad") == 0) return "traditional";
    return type;
}

static bool
rv_normalize_type(const char *input, char output[RV_NORMALIZED_NAME_MAX + 1])
{
    size_t length = 0;
    if (input == NULL || *input == '\0') {
        output[0] = '\0';
        return true;
    }
    while (*input != '\0') {
        unsigned char character = (unsigned char) *input++;
        if (character >= 0x80 || length == RV_NORMALIZED_NAME_MAX) return false;
        output[length++] = (char) rv_ascii_lower(character);
    }
    output[length] = '\0';
    const char *alias = rv_long_type_alias(output);
    if (alias != output) strcpy(output, alias);
    return true;
}

bool
rv_locale_open(const char *name, const char *collation_type, rv_locale *result)
{
    char normalized[RV_NORMALIZED_NAME_MAX + 1];
    char normalized_type[RV_NORMALIZED_NAME_MAX + 1];
    const char *modifier;
    int locale_index;
    int type_id;

    if (result == NULL || !rv_normalize_name(name, normalized)) return false;
    modifier = rv_embedded_modifier(name);
    if (strchr(name, '@') != NULL && modifier == NULL) return false;
    if (modifier != NULL && collation_type != NULL && *collation_type != '\0') {
        return false;
    }
    if ((collation_type == NULL || *collation_type == '\0') && modifier != NULL) {
        collation_type = modifier;
    }
    if (!rv_normalize_type(collation_type, normalized_type)) return false;

    if (strcmp(normalized, "c") == 0 || strcmp(normalized, "posix") == 0) {
        if (normalized_type[0] != '\0' && strcmp(normalized_type, "standard") != 0) {
            return false;
        }
        result->locale_index = RV_POSIX_LOCALE_INDEX;
        result->collation_profile = RV_POSIX_COLLATION_PROFILE;
        result->case_profile = 0;
        result->is_posix = 1;
        return true;
    }

    locale_index = rv_find_name(normalized, rv_locale_names,
            rv_locale_name_offsets, rv_locales_count);
    if (locale_index < 0) return false;

    result->locale_index = (uint16_t) locale_index;
    result->case_profile = rv_locales[locale_index].case_profile;
    result->is_posix = 0;
    if (normalized_type[0] == '\0') {
        result->collation_profile = rv_locales[locale_index].default_collation;
        return true;
    }

    type_id = rv_find_name(normalized_type, rv_type_names,
            rv_type_name_offsets, rv_type_name_offsets_count);
    if (type_id < 0) return false;
    {
        const rv_locale_row *locale = &rv_locales[locale_index];
        uint32_t low = locale->type_first;
        uint32_t high = low + locale->type_count;
        while (low < high) {
            uint32_t middle = low + (high - low) / 2;
            const rv_type_row *row = &rv_locale_types[middle];
            if (row->type_id == (uint16_t) type_id) {
                result->collation_profile = row->collation_profile;
                return true;
            }
            if (row->type_id > (uint16_t) type_id) high = middle;
            else low = middle + 1;
        }
    }
    return false;
}

int
rv_locale_class(const char *name)
{
    static const char *const names[] = {
        "alnum", "alpha", "blank", "cntrl", "digit", "graph",
        "lower", "print", "punct", "space", "upper", "xdigit"
    };
    size_t i;
    if (name == NULL) return -1;
    for (i = 0; i < sizeof(names) / sizeof(names[0]); i++) {
        if (strcmp(name, names[i]) == 0) return (int) i;
    }
    return -1;
}

static uint16_t
rv_posix_mask(uint32_t cp)
{
    bool upper = cp >= 'A' && cp <= 'Z';
    bool lower = cp >= 'a' && cp <= 'z';
    bool alpha = upper || lower;
    bool digit = cp >= '0' && cp <= '9';
    bool alnum = alpha || digit;
    bool blank = cp == ' ' || cp == '\t';
    bool space = blank || cp == '\n' || cp == '\v' || cp == '\f' || cp == '\r';
    bool cntrl = cp <= 0x1f || cp == 0x7f;
    bool print = cp >= 0x20 && cp <= 0x7e;
    bool graph = cp >= 0x21 && cp <= 0x7e;
    bool punct = graph && !alnum;
    bool xdigit = digit || (cp >= 'A' && cp <= 'F')
        || (cp >= 'a' && cp <= 'f');
    uint16_t mask = 0;
    if (alnum) mask |= 1u << RV_CLASS_ALNUM;
    if (alpha) mask |= 1u << RV_CLASS_ALPHA;
    if (blank) mask |= 1u << RV_CLASS_BLANK;
    if (cntrl) mask |= 1u << RV_CLASS_CNTRL;
    if (digit) mask |= 1u << RV_CLASS_DIGIT;
    if (graph) mask |= 1u << RV_CLASS_GRAPH;
    if (lower) mask |= 1u << RV_CLASS_LOWER;
    if (print) mask |= 1u << RV_CLASS_PRINT;
    if (punct) mask |= 1u << RV_CLASS_PUNCT;
    if (space) mask |= 1u << RV_CLASS_SPACE;
    if (upper) mask |= 1u << RV_CLASS_UPPER;
    if (xdigit) mask |= 1u << RV_CLASS_XDIGIT;
    return mask;
}

bool
rv_locale_isclass(const rv_locale *locale, rv_ctype_class character_class,
                  uint32_t code_point)
{
    uint16_t mask;
    if (locale == NULL || character_class < RV_CLASS_ALNUM
            || character_class > RV_CLASS_XDIGIT || !rv_valid_scalar(code_point)) {
        return false;
    }
    if (locale->is_posix) mask = rv_posix_mask(code_point);
    else mask = rv_ctype_blocks[(size_t) rv_ctype_stage1[code_point >> 8] * 256
            + (code_point & 0xff)];
    return (mask & (1u << character_class)) != 0;
}

static const rv_case_map *
rv_find_case(const rv_case_map *maps, uint32_t count, uint32_t code_point)
{
    uint32_t low = 0;
    uint32_t high = count;
    while (low < high) {
        uint32_t middle = low + (high - low) / 2;
        if (maps[middle].code_point == code_point) return &maps[middle];
        if (maps[middle].code_point > code_point) high = middle;
        else low = middle + 1;
    }
    return NULL;
}

static uint32_t
rv_case_convert(const rv_locale *locale, uint32_t code_point, bool upper)
{
    const rv_case_map *map;
    if (locale == NULL || !rv_valid_scalar(code_point)) return code_point;
    if (locale->is_posix) {
        if (upper && code_point >= 'a' && code_point <= 'z') return code_point - 32;
        if (!upper && code_point >= 'A' && code_point <= 'Z') return code_point + 32;
        return code_point;
    }
    if (locale->case_profile == 1) {
        map = rv_find_case(rv_case_turkic, rv_case_turkic_count, code_point);
        if (map != NULL) return upper ? map->upper : map->lower;
    }
    map = rv_find_case(rv_case_default, rv_case_default_count, code_point);
    if (map == NULL) return code_point;
    return upper ? map->upper : map->lower;
}

uint32_t
rv_locale_toupper(const rv_locale *locale, uint32_t code_point)
{
    return rv_case_convert(locale, code_point, true);
}

uint32_t
rv_locale_tolower(const rv_locale *locale, uint32_t code_point)
{
    return rv_case_convert(locale, code_point, false);
}

static int
rv_compare_sequence(const uint32_t *code_points, size_t length, uint32_t index)
{
    const rv_sequence_row *row = &rv_sequences[index];
    size_t common = length < row->length ? length : row->length;
    size_t i;
    for (i = 0; i < common; i++) {
        uint32_t stored = rv_sequence_codepoints[row->offset + i];
        if (code_points[i] < stored) return -1;
        if (code_points[i] > stored) return 1;
    }
    if (length < row->length) return -1;
    if (length > row->length) return 1;
    return 0;
}

static bool
rv_element_id(const uint32_t *code_points, size_t length, uint32_t *result)
{
    uint32_t low;
    uint32_t high;
    size_t i;
    if (code_points == NULL || length == 0) return false;
    for (i = 0; i < length; i++) {
        if (!rv_valid_scalar(code_points[i])) return false;
    }
    if (length == 1) {
        *result = code_points[0];
        return true;
    }
    low = 0;
    high = rv_sequences_count;
    while (low < high) {
        uint32_t middle = low + (high - low) / 2;
        int comparison = rv_compare_sequence(code_points, length, middle);
        if (comparison == 0) {
            *result = RV_FIRST_SEQUENCE_ID + middle;
            return true;
        }
        if (comparison < 0) high = middle;
        else low = middle + 1;
    }
    return false;
}

static bool
rv_u32_contains(const uint32_t *values, uint32_t count, uint32_t needle)
{
    uint32_t low = 0;
    uint32_t high = count;
    while (low < high) {
        uint32_t middle = low + (high - low) / 2;
        if (values[middle] == needle) return true;
        if (values[middle] > needle) high = middle;
        else low = middle + 1;
    }
    return false;
}

static bool
rv_is_contraction(const rv_locale *locale, uint32_t element)
{
    const rv_collation_profile *profile;
    bool root;
    if (locale->is_posix) return false;
    profile = &rv_collation_profiles[locale->collation_profile];
    if (rv_u32_contains(rv_contraction_adds + profile->add_first,
            profile->add_count, element)) return true;
    root = rv_u32_contains(rv_root_contractions, rv_root_contractions_count, element);
    if (!root) return false;
    return !rv_u32_contains(rv_contraction_removes + profile->remove_first,
            profile->remove_count, element);
}

bool
rv_locale_is_collating_element(const rv_locale *locale,
                               const uint32_t *code_points, size_t length)
{
    uint32_t element;
    if (locale == NULL || !rv_element_id(code_points, length, &element)) return false;
    if (length == 1) return true;
    return rv_is_contraction(locale, element);
}

size_t
rv_locale_collating_prefix(const rv_locale *locale,
                           const uint32_t *code_points, size_t length)
{
    size_t candidate;
    size_t maximum;
    if (locale == NULL || code_points == NULL || length == 0) return 0;
    maximum = length < rv_max_sequence_length ? length : rv_max_sequence_length;
    for (candidate = maximum; candidate >= 2; candidate--) {
        if (rv_locale_is_collating_element(locale, code_points, candidate)) return candidate;
    }
    return rv_locale_is_collating_element(locale, code_points, 1) ? 1 : 0;
}

static const rv_pair *
rv_find_pair(const rv_pair *pairs, uint32_t count, uint32_t element)
{
    uint32_t low = 0;
    uint32_t high = count;
    while (low < high) {
        uint32_t middle = low + (high - low) / 2;
        if (pairs[middle].element == element) return &pairs[middle];
        if (pairs[middle].element > element) high = middle;
        else low = middle + 1;
    }
    return NULL;
}

static uint64_t
rv_primary_token(const rv_locale *locale, uint32_t element)
{
    const rv_collation_profile *profile =
            &rv_collation_profiles[locale->collation_profile];
    const rv_pair *pair = rv_find_pair(
            rv_collation_overrides + profile->override_first,
            profile->override_count, element);
    if (pair != NULL) return UINT64_C(0x200000000) | pair->representative;
    pair = rv_find_pair(rv_root_equivalences, rv_root_equivalences_count, element);
    if (pair != NULL) return UINT64_C(0x100000000) | pair->representative;
    return element;
}

bool
rv_locale_primary_equal(const rv_locale *locale,
                        const uint32_t *left, size_t left_length,
                        const uint32_t *right, size_t right_length)
{
    uint32_t left_element;
    uint32_t right_element;
    if (!rv_locale_is_collating_element(locale, left, left_length)
            || !rv_locale_is_collating_element(locale, right, right_length)
            || !rv_element_id(left, left_length, &left_element)
            || !rv_element_id(right, right_length, &right_element)) return false;
    if (left_element == right_element) return true;
    if (locale->is_posix) return false;
    return rv_primary_token(locale, left_element)
            == rv_primary_token(locale, right_element);
}

bool
rv_locale_supports_ranges(const rv_locale *locale)
{
    return locale != NULL && locale->is_posix;
}

size_t
rv_locale_count(void)
{
    return rv_locales_count;
}

const char *
rv_locale_name(size_t index)
{
    if (index >= rv_locales_count) return NULL;
    return rv_locale_names + rv_locale_name_offsets[index];
}
