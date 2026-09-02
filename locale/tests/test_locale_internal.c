/* Include the implementation so this test can audit every generated row. */
#include "../rv_locale.c"

#include <assert.h>
#include <stdio.h>

static void
test_sorted_u32(const uint32_t *values, uint32_t count)
{
    uint32_t i;
    for (i = 1; i < count; i++) assert(values[i - 1] < values[i]);
}

static void
test_sorted_pairs(const rv_pair *pairs, uint32_t count)
{
    uint32_t i;
    for (i = 1; i < count; i++) assert(pairs[i - 1].element < pairs[i].element);
}

static void
test_all_locale_selectors(void)
{
    uint32_t locale_index;
    for (locale_index = 0; locale_index < rv_locales_count; locale_index++) {
        const rv_locale_row *row = &rv_locales[locale_index];
        const char *name = rv_locale_names + rv_locale_name_offsets[row->name_id];
        rv_locale locale;
        uint32_t i;
        assert(row->name_id == locale_index);
        assert(row->default_collation < rv_collation_profiles_count);
        assert(rv_locale_open(name, NULL, &locale));
        assert(locale.locale_index == locale_index);
        assert(locale.collation_profile == row->default_collation);
        for (i = 0; i < row->type_count; i++) {
            const rv_type_row *type = &rv_locale_types[row->type_first + i];
            const char *type_name = rv_type_names + rv_type_name_offsets[type->type_id];
            assert(type->collation_profile < rv_collation_profiles_count);
            if (i != 0) {
                assert(rv_locale_types[row->type_first + i - 1].type_id < type->type_id);
            }
            assert(rv_locale_open(name, type_name, &locale));
            assert(locale.collation_profile == type->collation_profile);
        }
    }
}

static void
test_all_tables(void)
{
    uint32_t i;
    test_sorted_u32(rv_root_contractions, rv_root_contractions_count);
    test_sorted_pairs(rv_root_equivalences, rv_root_equivalences_count);
    for (i = 0; i < rv_sequences_count; i++) {
        const rv_sequence_row *row = &rv_sequences[i];
        assert(row->length >= 2);
        assert(row->length <= rv_max_sequence_length);
        if (i != 0) {
            const rv_sequence_row *previous = &rv_sequences[i - 1];
            const uint32_t *cps = rv_sequence_codepoints + row->offset;
            assert(rv_compare_sequence(cps, row->length, i - 1) > 0);
            assert(previous->offset + previous->length == row->offset);
        }
    }
    for (i = 0; i < rv_collation_profiles_count; i++) {
        const rv_collation_profile *profile = &rv_collation_profiles[i];
        test_sorted_pairs(rv_collation_overrides + profile->override_first,
                profile->override_count);
        test_sorted_u32(rv_contraction_adds + profile->add_first, profile->add_count);
        test_sorted_u32(rv_contraction_removes + profile->remove_first,
                profile->remove_count);
    }
    for (i = 1; i < rv_case_default_count; i++) {
        assert(rv_case_default[i - 1].code_point < rv_case_default[i].code_point);
    }
    for (i = 1; i < rv_case_turkic_count; i++) {
        assert(rv_case_turkic[i - 1].code_point < rv_case_turkic[i].code_point);
    }
}

static void
test_all_ctype_relationships(void)
{
    rv_locale root;
    uint32_t cp;
    assert(rv_locale_open("root", NULL, &root));
    for (cp = 0; cp <= 0x10ffff; cp++) {
        bool alpha;
        bool digit;
        bool alnum;
        if (cp >= 0xd800 && cp <= 0xdfff) continue;
        alpha = rv_locale_isclass(&root, RV_CLASS_ALPHA, cp);
        digit = rv_locale_isclass(&root, RV_CLASS_DIGIT, cp);
        alnum = rv_locale_isclass(&root, RV_CLASS_ALNUM, cp);
        assert(alnum == (alpha || digit));
        if (digit) assert(cp >= '0' && cp <= '9');
        if (rv_locale_isclass(&root, RV_CLASS_XDIGIT, cp)) {
            assert(digit || (cp >= 'A' && cp <= 'F') || (cp >= 'a' && cp <= 'f'));
        }
        if (rv_locale_isclass(&root, RV_CLASS_UPPER, cp)
                || rv_locale_isclass(&root, RV_CLASS_LOWER, cp)) assert(alpha);
    }
}

int
main(void)
{
    test_all_locale_selectors();
    test_all_tables();
    test_all_ctype_relationships();
    puts("locale table invariants: ok");
    return 0;
}
