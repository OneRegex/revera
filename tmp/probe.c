#include <regex.h>
#include <stdio.h>
int main(void) {
    regex_t re;
    regmatch_t m[4];
    if (regcomp(&re, "(a|ab)(c|bcd)(d*)", REG_EXTENDED)) return 1;
    if (regexec(&re, "abcd", 4, m, 0)) return 1;
    for (int i = 0; i < 4; i++)
        printf("(%lld,%lld) ", (long long)m[i].rm_so, (long long)m[i].rm_eo);
    printf("\n");
    regfree(&re);
    return 0;
}
