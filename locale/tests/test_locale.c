#include "rv_locale.h"

#include <assert.h>
#include <stdio.h>

static void
test_lookup(void)
{
    rv_locale locale;
    size_t index;
    assert(rv_locale_count() == 1122);
    assert(rv_locale_open("C", NULL, &locale));
    assert(locale.is_posix);
    assert(rv_locale_open("POSIX.UTF-8", "standard", &locale));
    assert(locale.is_posix);
    assert(rv_locale_open("fr_CA.UTF-8", NULL, &locale));
    assert(!locale.is_posix);
    assert(rv_locale_open("fr_CA.UTF8", NULL, &locale));
    assert(rv_locale_open("de-CH", "phonebk", &locale));
    assert(rv_locale_open("es_MX@collation=traditional", NULL, &locale));
    assert(rv_locale_open("es-MX", "trad", &locale));
    assert(!rv_locale_open("not-a-cldr-locale", NULL, &locale));
    assert(!rv_locale_open("fr", "not-a-collation", &locale));
    assert(!rv_locale_open("fr.ISO-8859-1", NULL, &locale));
    assert(!rv_locale_open("fr.u-t-f-8", NULL, &locale));
    assert(!rv_locale_open("fr.UTF--8", NULL, &locale));
    assert(!rv_locale_open("fr.UTF-8-", NULL, &locale));
    assert(!rv_locale_open("fr@calendar=gregorian", NULL, &locale));
    assert(!rv_locale_open("fr@collation=", NULL, &locale));
    assert(!rv_locale_open("fr@collation=standard", "standard", &locale));
    assert(rv_locale_name(rv_locale_count()) == NULL);
    for (index = 0; index < rv_locale_count(); index++) {
        const char *name = rv_locale_name(index);
        assert(name != NULL);
        assert(rv_locale_open(name, NULL, &locale));
    }
}

static void
test_ctype(void)
{
    rv_locale posix;
    rv_locale greek;
    assert(rv_locale_open("C", NULL, &posix));
    assert(rv_locale_open("el", NULL, &greek));

    assert(rv_locale_class("alpha") == RV_CLASS_ALPHA);
    assert(rv_locale_class("notaclass") == -1);
    assert(rv_locale_isclass(&posix, RV_CLASS_ALPHA, 'Z'));
    assert(!rv_locale_isclass(&posix, RV_CLASS_ALPHA, 0x03b1));
    assert(rv_locale_isclass(&posix, RV_CLASS_SPACE, '\v'));
    assert(rv_locale_isclass(&posix, RV_CLASS_CNTRL, 0x7f));
    assert(rv_locale_isclass(&greek, RV_CLASS_ALPHA, 0x03b1));
    assert(rv_locale_isclass(&greek, RV_CLASS_UPPER, 0x0391));
    assert(rv_locale_isclass(&greek, RV_CLASS_SPACE, 0x2003));
    assert(rv_locale_isclass(&greek, RV_CLASS_BLANK, '\t'));
    assert(!rv_locale_isclass(&greek, RV_CLASS_PRINT, '\t'));

    /* POSIX requires digit and xdigit to remain the portable ASCII sets. */
    assert(!rv_locale_isclass(&greek, RV_CLASS_DIGIT, 0x0661));
    assert(!rv_locale_isclass(&greek, RV_CLASS_XDIGIT, 0xff21));
    assert(rv_locale_isclass(&greek, RV_CLASS_XDIGIT, 'f'));
}

static void
test_case(void)
{
    rv_locale english;
    rv_locale turkish;
    rv_locale azeri;
    assert(rv_locale_open("en", NULL, &english));
    assert(rv_locale_open("tr-TR", NULL, &turkish));
    assert(rv_locale_open("az-Latn-AZ", NULL, &azeri));

    assert(rv_locale_toupper(&english, 'i') == 'I');
    assert(rv_locale_tolower(&english, 'I') == 'i');
    assert(rv_locale_toupper(&turkish, 'i') == 0x0130);
    assert(rv_locale_tolower(&turkish, 'I') == 0x0131);
    assert(rv_locale_toupper(&azeri, 'i') == 0x0130);
    assert(rv_locale_tolower(&azeri, 0x0130) == 'i');

    /* Only one-scalar POSIX counterpart mappings are represented. */
    assert(rv_locale_toupper(&english, 0x00df) == 0x00df);
}

static void
test_collation(void)
{
    rv_locale posix;
    rv_locale root;
    rv_locale swedish;
    rv_locale spanish;
    rv_locale czech;
    const uint32_t a[] = {'a'};
    const uint32_t a_acute[] = {0x00e1};
    const uint32_t a_ring[] = {0x00e5};
    const uint32_t ch[] = {'c', 'h'};
    const uint32_t chaos[] = {'c', 'h', 'a', 'o', 's'};
    const uint32_t bogus[] = {'x', 'q'};

    assert(rv_locale_open("C", NULL, &posix));
    assert(rv_locale_open("root", NULL, &root));
    assert(rv_locale_open("sv-SE", NULL, &swedish));
    assert(rv_locale_open("es-ES", "traditional", &spanish));
    assert(rv_locale_open("cs-CZ", NULL, &czech));

    assert(rv_locale_is_collating_element(&posix, a, 1));
    assert(rv_locale_is_collating_element(&posix, a_acute, 1));
    assert(!rv_locale_is_collating_element(&posix, ch, 2));
    assert(!rv_locale_primary_equal(&posix, a, 1, a_acute, 1));
    assert(rv_locale_supports_ranges(&posix));
    assert(!rv_locale_supports_ranges(&swedish));

    assert(rv_locale_primary_equal(&root, a, 1, a_acute, 1));
    assert(rv_locale_primary_equal(&root, a, 1, a_ring, 1));
    assert(rv_locale_primary_equal(&swedish, a, 1, a_acute, 1));
    assert(!rv_locale_primary_equal(&swedish, a, 1, a_ring, 1));

    assert(rv_locale_is_collating_element(&spanish, ch, 2));
    assert(rv_locale_is_collating_element(&czech, ch, 2));
    assert(!rv_locale_is_collating_element(&root, ch, 2));
    assert(!rv_locale_is_collating_element(&spanish, bogus, 2));
    assert(rv_locale_collating_prefix(&spanish, chaos, 5) == 2);
    assert(rv_locale_collating_prefix(&root, chaos, 5) == 1);
}

int
main(void)
{
    test_lookup();
    test_ctype();
    test_case();
    test_collation();
    puts("locale tables: ok");
    return 0;
}
