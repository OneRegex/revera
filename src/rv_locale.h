#ifndef RV_LOCALE_H
#define RV_LOCALE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef enum rv_ctype_class {
    RV_CLASS_ALNUM = 0,
    RV_CLASS_ALPHA,
    RV_CLASS_BLANK,
    RV_CLASS_CNTRL,
    RV_CLASS_DIGIT,
    RV_CLASS_GRAPH,
    RV_CLASS_LOWER,
    RV_CLASS_PRINT,
    RV_CLASS_PUNCT,
    RV_CLASS_SPACE,
    RV_CLASS_UPPER,
    RV_CLASS_XDIGIT
} rv_ctype_class;

typedef struct rv_locale {
    uint16_t locale_index;
    uint16_t collation_profile;
    uint8_t case_profile;
    uint8_t is_posix;
} rv_locale;

/*
 * Opens a CLDR locale and optional collation type without consulting the host.
 * Locale names are ASCII case-insensitive and accept '-' or '_' separators and
 * an optional .UTF-8 suffix. C and POSIX select the POSIX locale. The collation
 * type is a CLDR long name such as "traditional" or its BCP 47 short alias.
 */
bool rv_locale_open(const char *name, const char *collation_type,
                    rv_locale *result);

/* Returns one of the twelve standard class identifiers, or -1 if unknown. */
int rv_locale_class(const char *name);

/* Tests one Unicode scalar against a standard LC_CTYPE class. */
bool rv_locale_isclass(const rv_locale *locale, rv_ctype_class character_class,
                       uint32_t code_point);

/* Returns the locale's one-character case counterpart, or the input itself. */
uint32_t rv_locale_toupper(const rv_locale *locale, uint32_t code_point);
uint32_t rv_locale_tolower(const rv_locale *locale, uint32_t code_point);

/* Tests whether a scalar sequence is one collating element in this locale. */
bool rv_locale_is_collating_element(const rv_locale *locale,
                                    const uint32_t *code_points, size_t length);

/* Returns the longest collating-element prefix, or zero for invalid input. */
size_t rv_locale_collating_prefix(const rv_locale *locale,
                                  const uint32_t *code_points, size_t length);

/* Tests primary LC_COLLATE equivalence between two collating elements. */
bool rv_locale_primary_equal(const rv_locale *locale,
                             const uint32_t *left, size_t left_length,
                             const uint32_t *right, size_t right_length);

/* Non-POSIX locale ranges intentionally use the permitted reject policy. */
bool rv_locale_supports_ranges(const rv_locale *locale);

/* The generated CLDR locale count, excluding the C and POSIX aliases. */
size_t rv_locale_count(void);

/* Returns the normalized CLDR name at index, or NULL when out of range. */
const char *rv_locale_name(size_t index);

#ifdef __cplusplus
}
#endif

#endif
