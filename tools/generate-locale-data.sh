#!/bin/sh
set -eu

if [ "$#" -lt 3 ] || [ "$#" -gt 4 ]; then
    echo "usage: $0 CLDR_COMMON_ZIP CLDR_TOOLS_JAR JAVA_HOME [OUTPUT]" >&2
    exit 2
fi

common_zip=$1
tools_jar=$2
java_home=$3
output=${4:-src/rv_locale_data.inc}

digest_file() {
    if command -v sha512sum >/dev/null 2>&1; then
        sha512sum "$1" | cut -d ' ' -f 1
    else
        shasum -a 512 "$1" | cut -d ' ' -f 1
    fi
}

verify_file() {
    expected=$1
    path=$2
    actual=$(digest_file "$path")
    if [ "$actual" != "$expected" ]; then
        echo "SHA-512 mismatch for $path: $actual" >&2
        exit 1
    fi
}

verify_file \
    de8660f5371e0fcfd03a42e3b4fc4c686ec6cd602b402f1e3d227844005a54eb7952873894443523837d5828c42874a1a267a19f91ded207a2d166144791fa62 \
    "$common_zip"
verify_file \
    4dd00bed4ea525d834cc50b03ab74e6e0b553fed2b16fc9312d6e3b80712cd3b90ffa69a6bf9c6288c16aae9812225a488999dfc75dbaeb32d6c36d8a9af307f \
    "$tools_jar"

build_dir=$(mktemp -d "${TMPDIR:-/tmp}/re-vera2-locale.XXXXXX")
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM

"$java_home/bin/javac" -Xlint:all -Werror -cp "$tools_jar" \
    -d "$build_dir" tools/GenerateLocaleData.java
"$java_home/bin/java" -Xmx4g -cp "$tools_jar:$build_dir" \
    GenerateLocaleData "$common_zip" "$output"
