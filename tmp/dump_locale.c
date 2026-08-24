#include <stdio.h>
#include <string.h>
#include "../src/rv_locale.h"

static void dump_locale(const char *name, const char *type) {
    rv_locale l;
    if (!rv_locale_open(name, type, &l)) { printf("open %s/%s FAIL\n", name, type); return; }
    printf("locale %s/%s\n", name, type);
    for (unsigned cp = 0; cp < 0x500; cp += 7) {
        unsigned mask = 0;
        for (int c = 0; c < 12; c++)
            if (rv_locale_isclass(&l, (rv_ctype_class)c, cp)) mask |= 1u << c;
        printf("c %04x %03x %04x %04x\n", cp, mask,
               (unsigned)rv_locale_toupper(&l, cp), (unsigned)rv_locale_tolower(&l, cp));
    }
    for (unsigned cp = 0x1E00; cp < 0x1F00; cp += 3) {
        printf("u %04x %04x %04x\n", cp,
               (unsigned)rv_locale_toupper(&l, cp), (unsigned)rv_locale_tolower(&l, cp));
    }
    const char *pairs[][2] = {
        {"a","à"},{"a","á"},{"a","A"},{"o","ö"},{"u","ü"},{"c","ç"},{"e","é"},
        {"s","ß"},{"n","ñ"},{"a","b"},{"i","ı"},{"i","İ"}
    };
    for (size_t i = 0; i < sizeof(pairs)/sizeof(pairs[0]); i++) {
        uint32_t left[4], right[4]; size_t ln = 0, rn = 0;
        const unsigned char *s = (const unsigned char*)pairs[i][0];
        /* crude UTF-8 decode for the short test strings */
        for (; *s; ) {
            uint32_t cp; int n;
            if (*s < 0x80) { cp = *s; n = 1; }
            else if ((*s >> 5) == 6) { cp = (*s & 0x1f) << 6 | (s[1] & 0x3f); n = 2; }
            else { cp = (*s & 0x0f) << 12 | (s[1] & 0x3f) << 6 | (s[2] & 0x3f); n = 3; }
            left[ln++] = cp; s += n;
        }
        s = (const unsigned char*)pairs[i][1];
        for (; *s; ) {
            uint32_t cp; int n;
            if (*s < 0x80) { cp = *s; n = 1; }
            else if ((*s >> 5) == 6) { cp = (*s & 0x1f) << 6 | (s[1] & 0x3f); n = 2; }
            else { cp = (*s & 0x0f) << 12 | (s[1] & 0x3f) << 6 | (s[2] & 0x3f); n = 3; }
            right[rn++] = cp; s += n;
        }
        printf("eq %s %s %d\n", pairs[i][0], pairs[i][1],
               rv_locale_primary_equal(&l, left, ln, right, rn));
    }
    const char *elems[] = {"ch","dz","ll","dzs","ngb","ab"};
    for (size_t i = 0; i < sizeof(elems)/sizeof(elems[0]); i++) {
        uint32_t cps[8]; size_t n = strlen(elems[i]);
        for (size_t k = 0; k < n; k++) cps[k] = (uint32_t)elems[i][k];
        printf("ce %s %d %zu\n", elems[i],
               rv_locale_is_collating_element(&l, cps, n),
               rv_locale_collating_prefix(&l, cps, n));
    }
}

int main(void) {
    const char *locales[][2] = {
        {"C",""},{"en",""},{"en-US",""},{"fr",""},{"de",""},{"de","phonebook"},
        {"tr",""},{"az",""},{"cs",""},{"hu",""},{"es",""},{"es","traditional"},
        {"da",""},{"sv",""},{"ja",""},{"zh",""},{"zh","pinyin"},{"vi",""},{"sl",""}
    };
    for (size_t i = 0; i < sizeof(locales)/sizeof(locales[0]); i++)
        dump_locale(locales[i][0], locales[i][1]);
    return 0;
}
