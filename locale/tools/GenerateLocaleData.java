import com.ibm.icu.lang.UCharacter;
import com.ibm.icu.lang.UProperty;
import com.ibm.icu.text.Collator;
import com.ibm.icu.text.RawCollationKey;
import com.ibm.icu.text.RuleBasedCollator;
import com.ibm.icu.text.UnicodeSet;
import com.ibm.icu.util.ULocale;

import java.io.BufferedWriter;
import java.io.IOException;
import java.io.OutputStreamWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.TreeSet;
import java.util.zip.ZipFile;

/** Generates the locale tables used by the ERE runtime. */
public final class GenerateLocaleData {
    private static final int MAX_CP = 0x10ffff;
    private static final int FIRST_SEQUENCE_ID = 0x110000;

    private static final int ALNUM = 1 << 0;
    private static final int ALPHA = 1 << 1;
    private static final int BLANK = 1 << 2;
    private static final int CNTRL = 1 << 3;
    private static final int DIGIT = 1 << 4;
    private static final int GRAPH = 1 << 5;
    private static final int LOWER = 1 << 6;
    private static final int PRINT = 1 << 7;
    private static final int PUNCT = 1 << 8;
    private static final int SPACE = 1 << 9;
    private static final int UPPER = 1 << 10;
    private static final int XDIGIT = 1 << 11;

    private record ByteKey(byte[] bytes) {
        @Override public boolean equals(Object other) {
            return other instanceof ByteKey key && Arrays.equals(bytes, key.bytes);
        }
        @Override public int hashCode() { return Arrays.hashCode(bytes); }
    }

    private record CaseMap(int codePoint, int upper, int lower) {}
    private record Pair(int element, int representative) {}
    private record Sequence(int[] codePoints) {}

    private record ProfileSpec(String fingerprint, ULocale selector) {}

    private record SelectorSpec(String locale, String type, String fingerprint) {}

    private static final class CollationData {
        final Pair[] overrides;
        final int[] contractionAdds;
        final int[] contractionRemoves;

        CollationData(Pair[] overrides, int[] contractionAdds, int[] contractionRemoves) {
            this.overrides = overrides;
            this.contractionAdds = contractionAdds;
            this.contractionRemoves = contractionRemoves;
        }

        @Override public boolean equals(Object other) {
            if (!(other instanceof CollationData data)) return false;
            return Arrays.equals(overrides, data.overrides)
                    && Arrays.equals(contractionAdds, data.contractionAdds)
                    && Arrays.equals(contractionRemoves, data.contractionRemoves);
        }

        @Override public int hashCode() {
            int hash = Arrays.hashCode(overrides);
            hash = 31 * hash + Arrays.hashCode(contractionAdds);
            return 31 * hash + Arrays.hashCode(contractionRemoves);
        }
    }

    private static final class ProfileResult {
        final TreeMap<Integer, Integer> membership;
        final int[] contractions;

        ProfileResult(TreeMap<Integer, Integer> membership, int[] contractions) {
            this.membership = membership;
            this.contractions = contractions;
        }
    }

    private record CTypeData(int[] stage1, List<int[]> blocks) {}

    private static final class LocaleRow {
        final String name;
        final int caseProfile;
        final List<TypeRow> types = new ArrayList<>();

        LocaleRow(String name, int caseProfile) {
            this.name = name;
            this.caseProfile = caseProfile;
        }
    }

    private record TypeRow(String type, int profile) {}

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            System.err.println("usage: GenerateLocaleData CLDR_COMMON_ZIP OUTPUT_INC");
            System.exit(2);
        }
        new GenerateLocaleData().run(Path.of(args[0]), Path.of(args[1]));
    }

    private void run(Path commonZip, Path output) throws Exception {
        TreeSet<String> localeNames = readLocaleNames(commonZip);
        if (localeNames.size() != 1122 || !localeNames.contains("root")) {
            throw new IllegalStateException(
                    "expected 1122 CLDR 48.2 main locales including root, got "
                            + localeNames.size());
        }

        List<SelectorSpec> selectors = new ArrayList<>();
        TreeMap<String, ProfileSpec> profileSpecs = new TreeMap<>();
        Map<String, String> descriptors = new HashMap<>();
        for (String locale : localeNames) {
            ULocale uLocale = new ULocale(locale);
            TreeSet<String> types = new TreeSet<>();
            types.add("");
            types.addAll(Arrays.asList(
                    Collator.getKeywordValuesForLocale("collation", uLocale, false)));
            for (String type : types) {
                ULocale selected = type.isEmpty() ? uLocale
                        : new ULocale(locale + "@collation=" + type);
                RuleBasedCollator collator = thawedPrimary(selected);
                String descriptor = profileDescriptor(collator);
                String fingerprint = sha256(descriptor);
                String old = descriptors.putIfAbsent(fingerprint, descriptor);
                if (old != null && !old.equals(descriptor)) {
                    throw new IllegalStateException("SHA-256 collision in collation profiles");
                }
                profileSpecs.putIfAbsent(fingerprint, new ProfileSpec(fingerprint, selected));
                selectors.add(new SelectorSpec(normalizeLocale(locale), type, fingerprint));
            }
        }

        RuleBasedCollator rootCollator = thawedPrimary(ULocale.ROOT);
        String rootFingerprint = sha256(profileDescriptor(rootCollator));
        ProfileSpec rootSpec = profileSpecs.get(rootFingerprint);
        if (rootSpec == null) throw new IllegalStateException("root profile is missing");

        List<ProfileSpec> orderedProfiles = new ArrayList<>();
        orderedProfiles.add(rootSpec);
        for (ProfileSpec spec : profileSpecs.values()) {
            if (!spec.fingerprint.equals(rootFingerprint)) orderedProfiles.add(spec);
        }

        TreeSet<Sequence> sequenceSet = new TreeSet<>(GenerateLocaleData::compareSequences);
        Map<String, int[]> contractionsByFingerprint = new HashMap<>();
        for (ProfileSpec spec : orderedProfiles) {
            RuleBasedCollator collator = thawedPrimary(spec.selector);
            UnicodeSet contractions = new UnicodeSet();
            collator.getContractionsAndExpansions(contractions, new UnicodeSet(), false);
            List<int[]> profileSequences = new ArrayList<>();
            for (String string : contractions.strings()) {
                int[] codePoints = string.codePoints().toArray();
                if (codePoints.length < 2) {
                    throw new IllegalStateException("ICU returned a one-character contraction");
                }
                sequenceSet.add(new Sequence(codePoints));
                profileSequences.add(codePoints);
            }
            profileSequences.sort(GenerateLocaleData::compareCodePointArrays);
            contractionsByFingerprint.put(spec.fingerprint,
                    flattenSequencesForLookup(profileSequences));
        }

        List<Sequence> sequences = new ArrayList<>(sequenceSet);
        Map<String, Integer> sequenceIds = new HashMap<>();
        for (int i = 0; i < sequences.size(); i++) {
            sequenceIds.put(sequenceKey(sequences.get(i).codePoints), FIRST_SEQUENCE_ID + i);
        }

        Map<String, int[]> contractionIds = new HashMap<>();
        for (ProfileSpec spec : orderedProfiles) {
            int[] flattened = contractionsByFingerprint.get(spec.fingerprint);
            int count = flattened[0];
            int[] ids = new int[count];
            int cursor = 1;
            for (int i = 0; i < count; i++) {
                int length = flattened[cursor++];
                int[] cps = Arrays.copyOfRange(flattened, cursor, cursor + length);
                cursor += length;
                ids[i] = sequenceIds.get(sequenceKey(cps));
            }
            Arrays.sort(ids);
            contractionIds.put(spec.fingerprint, ids);
        }

        ProfileResult root = buildProfile(rootCollator,
                contractionIds.get(rootFingerprint), sequences);
        Map<String, Integer> fingerprintToDataProfile = new HashMap<>();
        List<CollationData> dataProfiles = new ArrayList<>();
        dataProfiles.add(new CollationData(new Pair[0], new int[0], new int[0]));
        fingerprintToDataProfile.put(rootFingerprint, 0);

        Map<Integer, int[]> rootGroups = groups(root.membership);
        for (int profileIndex = 1; profileIndex < orderedProfiles.size(); profileIndex++) {
            ProfileSpec spec = orderedProfiles.get(profileIndex);
            RuleBasedCollator collator = thawedPrimary(spec.selector);
            ProfileResult result = buildProfile(collator,
                    contractionIds.get(spec.fingerprint), sequences);
            Pair[] overrides = differences(root.membership, rootGroups, result.membership);
            int[] adds = subtractSorted(result.contractions, root.contractions);
            int[] removes = subtractSorted(root.contractions, result.contractions);
            verifyDelta(root.membership, result.membership, overrides);
            verifyContractions(root.contractions, result.contractions, adds, removes);
            CollationData data = new CollationData(overrides, adds, removes);
            int dataId = dataProfiles.indexOf(data);
            if (dataId < 0) {
                dataId = dataProfiles.size();
                dataProfiles.add(data);
            }
            fingerprintToDataProfile.put(spec.fingerprint, dataId);
            System.err.printf(Locale.ROOT,
                    "profile %3d/%d %-24s members=%d overrides=%d +%d -%d data=%d%n",
                    profileIndex + 1, orderedProfiles.size(),
                    collator.getLocale(ULocale.ACTUAL_LOCALE), result.membership.size(),
                    overrides.length, adds.length, removes.length, dataId);
        }

        TreeMap<String, LocaleRow> localeRows = new TreeMap<>();
        for (String locale : localeNames) {
            String normalized = normalizeLocale(locale);
            localeRows.put(normalized, new LocaleRow(normalized,
                    isTurkic(locale) ? 1 : 0));
        }
        for (SelectorSpec selector : selectors) {
            LocaleRow row = localeRows.get(selector.locale);
            row.types.add(new TypeRow(selector.type,
                    fingerprintToDataProfile.get(selector.fingerprint)));
        }
        for (LocaleRow row : localeRows.values()) {
            row.types.sort(Comparator.comparing(TypeRow::type));
        }

        CTypeData ctype = buildCType();
        List<CaseMap> defaultCase = buildDefaultCaseMaps();
        List<CaseMap> turkicCase = buildTurkicOverrides(defaultCase);
        emit(output, localeRows, dataProfiles, root, sequences, ctype,
                defaultCase, turkicCase);

        System.err.printf(Locale.ROOT,
                "generated locales=%d selectors=%d sourceProfiles=%d dataProfiles=%d "
                        + "sequences=%d rootEquivalences=%d ctypeBlocks=%d%n",
                localeRows.size(), selectors.size(), orderedProfiles.size(),
                dataProfiles.size(), sequences.size(), root.membership.size(),
                ctype.blocks.size());
    }

    private static TreeSet<String> readLocaleNames(Path zipPath) throws IOException {
        TreeSet<String> locales = new TreeSet<>();
        try (ZipFile zip = new ZipFile(zipPath.toFile())) {
            zip.stream().forEach(entry -> {
                String name = entry.getName();
                if (name.matches("common/main/[^/]+\\.xml")) {
                    locales.add(name.substring(12, name.length() - 4));
                }
            });
        }
        return locales;
    }

    private static String normalizeLocale(String locale) {
        return locale.toLowerCase(Locale.ROOT).replace('_', '-');
    }

    private static boolean isTurkic(String locale) {
        String language = locale.split("[_-]", 2)[0];
        return language.equals("tr") || language.equals("az");
    }

    private static RuleBasedCollator thawedPrimary(ULocale locale) {
        RuleBasedCollator collator = (RuleBasedCollator) Collator.getInstance(locale);
        collator = collator.cloneAsThawed();
        collator.setStrength(Collator.PRIMARY);
        /* A case level is separate from the primary collation weight. */
        collator.setCaseLevel(false);
        collator.freeze();
        return collator;
    }

    private static String profileDescriptor(RuleBasedCollator collator) {
        return collator.getRules(true) + "\n--settings--\n"
                + collator.getDecomposition() + "|"
                + Arrays.toString(collator.getReorderCodes()) + "|"
                + collator.isAlternateHandlingShifted() + "|"
                + collator.getMaxVariable() + "|"
                + collator.getNumericCollation();
    }

    private static String sha256(String value) throws Exception {
        byte[] digest = MessageDigest.getInstance("SHA-256")
                .digest(value.getBytes(StandardCharsets.UTF_8));
        StringBuilder result = new StringBuilder(64);
        for (byte b : digest) result.append(String.format(Locale.ROOT, "%02x", b & 0xff));
        return result.toString();
    }

    private static int compareSequences(Sequence left, Sequence right) {
        return compareCodePointArrays(left.codePoints, right.codePoints);
    }

    private static int compareCodePointArrays(int[] left, int[] right) {
        int common = Math.min(left.length, right.length);
        for (int i = 0; i < common; i++) {
            int comparison = Integer.compare(left[i], right[i]);
            if (comparison != 0) return comparison;
        }
        return Integer.compare(left.length, right.length);
    }

    /* Stores [count, length, code points..., length, code points...] temporarily. */
    private static int[] flattenSequencesForLookup(List<int[]> sequences) {
        int size = 1;
        for (int[] sequence : sequences) size += 1 + sequence.length;
        int[] result = new int[size];
        result[0] = sequences.size();
        int cursor = 1;
        for (int[] sequence : sequences) {
            result[cursor++] = sequence.length;
            for (int cp : sequence) result[cursor++] = cp;
        }
        return result;
    }

    private static String sequenceKey(int[] codePoints) {
        StringBuilder result = new StringBuilder(codePoints.length * 7);
        for (int cp : codePoints) result.append(cp).append(',');
        return result.toString();
    }

    private static ProfileResult buildProfile(RuleBasedCollator collator,
                                              int[] contractions,
                                              List<Sequence> allSequences) {
        Map<ByteKey, Integer> counts = new HashMap<>(1_500_000);
        RawCollationKey raw = new RawCollationKey(32);
        for (int cp = 0; cp <= MAX_CP; cp++) {
            if (cp < 0xd800 || cp > 0xdfff) {
                raw = countKey(collator, new String(Character.toChars(cp)), raw, counts);
            }
        }
        for (int id : contractions) {
            int[] cps = allSequences.get(id - FIRST_SEQUENCE_ID).codePoints;
            raw = countKey(collator, new String(cps, 0, cps.length), raw, counts);
        }

        Map<ByteKey, Integer> representatives = new HashMap<>();
        TreeMap<Integer, Integer> membership = new TreeMap<>();
        for (int cp = 0; cp <= MAX_CP; cp++) {
            if (cp < 0xd800 || cp > 0xdfff) {
                raw = collectMember(collator, new String(Character.toChars(cp)), cp,
                        raw, counts, representatives, membership);
            }
        }
        for (int id : contractions) {
            int[] cps = allSequences.get(id - FIRST_SEQUENCE_ID).codePoints;
            raw = collectMember(collator, new String(cps, 0, cps.length), id,
                    raw, counts, representatives, membership);
        }
        return new ProfileResult(membership, contractions);
    }

    private static RawCollationKey countKey(RuleBasedCollator collator, String value,
                                            RawCollationKey raw,
                                            Map<ByteKey, Integer> counts) {
        raw = collator.getRawCollationKey(value, raw);
        ByteKey key = copyKey(raw);
        counts.merge(key, 1, Integer::sum);
        return raw;
    }

    private static RawCollationKey collectMember(RuleBasedCollator collator, String value,
                                                 int element, RawCollationKey raw,
                                                 Map<ByteKey, Integer> counts,
                                                 Map<ByteKey, Integer> representatives,
                                                 TreeMap<Integer, Integer> membership) {
        raw = collator.getRawCollationKey(value, raw);
        ByteKey key = copyKey(raw);
        if (counts.get(key) > 1) {
            int representative = representatives.computeIfAbsent(key, ignored -> element);
            membership.put(element, representative);
        }
        return raw;
    }

    private static ByteKey copyKey(RawCollationKey raw) {
        if (raw.size < 1 || raw.bytes[raw.size - 1] != 0) {
            throw new IllegalStateException("invalid ICU sort key");
        }
        return new ByteKey(Arrays.copyOf(raw.bytes, raw.size - 1));
    }

    private static Map<Integer, int[]> groups(TreeMap<Integer, Integer> membership) {
        TreeMap<Integer, List<Integer>> lists = new TreeMap<>();
        membership.forEach((element, representative) ->
                lists.computeIfAbsent(representative, ignored -> new ArrayList<>()).add(element));
        Map<Integer, int[]> result = new HashMap<>();
        lists.forEach((representative, members) ->
                result.put(representative, members.stream().mapToInt(Integer::intValue).toArray()));
        return result;
    }

    private static Pair[] differences(TreeMap<Integer, Integer> rootMembership,
                                      Map<Integer, int[]> rootGroups,
                                      TreeMap<Integer, Integer> membership) {
        Map<Integer, int[]> profileGroups = groups(membership);
        TreeSet<Integer> elements = new TreeSet<>(rootMembership.keySet());
        elements.addAll(membership.keySet());
        List<Pair> result = new ArrayList<>();
        for (int element : elements) {
            Integer rootRepresentative = rootMembership.get(element);
            Integer profileRepresentative = membership.get(element);
            boolean same;
            if (rootRepresentative == null || profileRepresentative == null) {
                same = rootRepresentative == null && profileRepresentative == null;
            } else {
                same = Arrays.equals(rootGroups.get(rootRepresentative),
                        profileGroups.get(profileRepresentative));
            }
            if (!same) {
                result.add(new Pair(element,
                        profileRepresentative == null ? element : profileRepresentative));
            }
        }
        return result.toArray(Pair[]::new);
    }

    private static int[] subtractSorted(int[] left, int[] right) {
        int[] result = new int[left.length];
        int i = 0, j = 0, count = 0;
        while (i < left.length) {
            while (j < right.length && right[j] < left[i]) j++;
            if (j == right.length || left[i] != right[j]) result[count++] = left[i];
            i++;
        }
        return Arrays.copyOf(result, count);
    }

    private static void verifyDelta(TreeMap<Integer, Integer> root,
                                    TreeMap<Integer, Integer> expected,
                                    Pair[] overrides) {
        Map<Integer, Integer> overrideMap = new HashMap<>();
        for (Pair pair : overrides) overrideMap.put(pair.element, pair.representative);
        TreeSet<Integer> elements = new TreeSet<>(root.keySet());
        elements.addAll(expected.keySet());
        Map<Long, Integer> tokenToGroup = new HashMap<>();
        Map<Integer, Long> groupToToken = new HashMap<>();
        for (int element : elements) {
            Integer override = overrideMap.get(element);
            Integer rootRepresentative = root.get(element);
            long token = override != null ? 0x200000000L | override
                    : rootRepresentative != null ? 0x100000000L | rootRepresentative
                    : element;
            int group = expected.getOrDefault(element, element);
            Integer oldGroup = tokenToGroup.putIfAbsent(token, group);
            Long oldToken = groupToToken.putIfAbsent(group, token);
            if (oldGroup != null && oldGroup != group
                    || oldToken != null && oldToken != token) {
                throw new IllegalStateException("collation delta changed equivalence semantics");
            }
        }
    }

    private static void verifyContractions(int[] root, int[] expected,
                                           int[] adds, int[] removes) {
        TreeSet<Integer> reconstructed = new TreeSet<>();
        for (int element : root) reconstructed.add(element);
        for (int element : removes) reconstructed.remove(element);
        for (int element : adds) reconstructed.add(element);
        int[] actual = reconstructed.stream().mapToInt(Integer::intValue).toArray();
        if (!Arrays.equals(expected, actual)) {
            throw new IllegalStateException("collation delta changed contractions");
        }
    }

    private static CTypeData buildCType() {
        List<int[]> blocks = new ArrayList<>();
        Map<BlockKey, Integer> blockIds = new HashMap<>();
        int[] stage1 = new int[0x1100];
        for (int high = 0; high < stage1.length; high++) {
            int[] block = new int[256];
            for (int low = 0; low < 256; low++) {
                block[low] = classMask((high << 8) | low);
            }
            BlockKey key = new BlockKey(block);
            Integer id = blockIds.get(key);
            if (id == null) {
                id = blocks.size();
                blocks.add(block);
                blockIds.put(key, id);
            }
            stage1[high] = id;
        }
        if (blocks.size() > 0xffff) throw new IllegalStateException("too many ctype blocks");
        return new CTypeData(stage1, blocks);
    }

    private record BlockKey(int[] values) {
        @Override public boolean equals(Object other) {
            return other instanceof BlockKey key && Arrays.equals(values, key.values);
        }
        @Override public int hashCode() { return Arrays.hashCode(values); }
    }

    private static int classMask(int cp) {
        if (cp >= 0xd800 && cp <= 0xdfff) return 0;
        int type = UCharacter.getType(cp);
        boolean alpha = UCharacter.hasBinaryProperty(cp, UProperty.ALPHABETIC);
        boolean upper = UCharacter.hasBinaryProperty(cp, UProperty.UPPERCASE);
        boolean lower = UCharacter.hasBinaryProperty(cp, UProperty.LOWERCASE);
        boolean digit = cp >= '0' && cp <= '9';
        boolean xdigit = digit || cp >= 'A' && cp <= 'F' || cp >= 'a' && cp <= 'f';
        boolean space = UCharacter.hasBinaryProperty(cp, UProperty.WHITE_SPACE);
        boolean blank = UCharacter.hasBinaryProperty(cp, UProperty.POSIX_BLANK);
        boolean cntrl = type == UCharacter.CONTROL;
        boolean graph = UCharacter.hasBinaryProperty(cp, UProperty.POSIX_GRAPH);
        boolean print = UCharacter.hasBinaryProperty(cp, UProperty.POSIX_PRINT);
        boolean punct = graph && !alpha && !digit;
        int mask = 0;
        if (alpha || digit) mask |= ALNUM;
        if (alpha) mask |= ALPHA;
        if (blank) mask |= BLANK;
        if (cntrl) mask |= CNTRL;
        if (digit) mask |= DIGIT;
        if (graph) mask |= GRAPH;
        if (lower) mask |= LOWER;
        if (print) mask |= PRINT;
        if (punct) mask |= PUNCT;
        if (space) mask |= SPACE;
        if (upper) mask |= UPPER;
        if (xdigit) mask |= XDIGIT;
        return mask;
    }

    private static List<CaseMap> buildDefaultCaseMaps() {
        List<CaseMap> result = new ArrayList<>();
        for (int cp = 0; cp <= MAX_CP; cp++) {
            if (cp >= 0xd800 && cp <= 0xdfff) continue;
            int upper = UCharacter.toUpperCase(cp);
            int lower = UCharacter.toLowerCase(cp);
            if (upper != cp || lower != cp) result.add(new CaseMap(cp, upper, lower));
        }
        return result;
    }

    private static List<CaseMap> buildTurkicOverrides(List<CaseMap> defaults) {
        Map<Integer, CaseMap> byCodePoint = new HashMap<>();
        for (CaseMap map : defaults) byCodePoint.put(map.codePoint, map);
        ULocale turkish = ULocale.forLanguageTag("tr");
        List<CaseMap> result = new ArrayList<>();
        for (int cp : new int[]{0x0049, 0x0069, 0x0130, 0x0131}) {
            String value = new String(Character.toChars(cp));
            int upper = singleCodePoint(UCharacter.toUpperCase(turkish, value), cp);
            int lower = singleCodePoint(UCharacter.toLowerCase(turkish, value), cp);
            CaseMap old = byCodePoint.getOrDefault(cp, new CaseMap(cp, cp, cp));
            if (upper != old.upper || lower != old.lower) {
                result.add(new CaseMap(cp, upper, lower));
            }
        }
        return result;
    }

    private static int singleCodePoint(String value, int fallback) {
        return value.codePointCount(0, value.length()) == 1 ? value.codePointAt(0) : fallback;
    }

    private static void emit(Path output, TreeMap<String, LocaleRow> locales,
                             List<CollationData> profiles, ProfileResult root,
                             List<Sequence> sequences, CTypeData ctype,
                             List<CaseMap> defaultCase, List<CaseMap> turkicCase)
            throws IOException {
        Files.createDirectories(output.toAbsolutePath().getParent());
        try (BufferedWriter writer = Files.newBufferedWriter(output, StandardCharsets.UTF_8)) {
            writer.write("/* Generated by locale/tools/GenerateLocaleData.java from CLDR 48.2. */\n");
            writer.write("/* ICU 78.2, Unicode 17.0.0. Do not edit. */\n\n");

            emitU16Array(writer, "rv_ctype_stage1", ctype.stage1);
            int[] flatBlocks = new int[ctype.blocks.size() * 256];
            for (int i = 0; i < ctype.blocks.size(); i++) {
                System.arraycopy(ctype.blocks.get(i), 0, flatBlocks, i * 256, 256);
            }
            emitU16Array(writer, "rv_ctype_blocks", flatBlocks);
            emitCaseMaps(writer, "rv_case_default", defaultCase);
            emitCaseMaps(writer, "rv_case_turkic", turkicCase);

            List<Integer> sequenceCodePoints = new ArrayList<>();
            List<int[]> sequenceRows = new ArrayList<>();
            int maxSequenceLength = 0;
            for (Sequence sequence : sequences) {
                int offset = sequenceCodePoints.size();
                for (int cp : sequence.codePoints) sequenceCodePoints.add(cp);
                sequenceRows.add(new int[]{offset, sequence.codePoints.length});
                maxSequenceLength = Math.max(maxSequenceLength, sequence.codePoints.length);
            }
            emitU32Array(writer, "rv_sequence_codepoints",
                    sequenceCodePoints.stream().mapToInt(Integer::intValue).toArray());
            emitSequenceRows(writer, sequenceRows);
            writer.write("static const uint16_t rv_max_sequence_length = "
                    + maxSequenceLength + ";\n\n");

            emitU32Array(writer, "rv_root_contractions", root.contractions);
            emitPairs(writer, "rv_root_equivalences",
                    root.membership.entrySet().stream()
                            .map(e -> new Pair(e.getKey(), e.getValue())).toArray(Pair[]::new));

            List<Pair> overrides = new ArrayList<>();
            List<Integer> adds = new ArrayList<>();
            List<Integer> removes = new ArrayList<>();
            List<int[]> profileRows = new ArrayList<>();
            for (CollationData profile : profiles) {
                profileRows.add(new int[]{overrides.size(), profile.overrides.length,
                        adds.size(), profile.contractionAdds.length,
                        removes.size(), profile.contractionRemoves.length});
                Collections.addAll(overrides, profile.overrides);
                for (int value : profile.contractionAdds) adds.add(value);
                for (int value : profile.contractionRemoves) removes.add(value);
            }
            emitPairs(writer, "rv_collation_overrides", overrides.toArray(Pair[]::new));
            emitU32Array(writer, "rv_contraction_adds",
                    adds.stream().mapToInt(Integer::intValue).toArray());
            emitU32Array(writer, "rv_contraction_removes",
                    removes.stream().mapToInt(Integer::intValue).toArray());
            emitProfileRows(writer, profileRows);

            TreeSet<String> typeNames = new TreeSet<>();
            typeNames.add("");
            for (LocaleRow locale : locales.values()) {
                for (TypeRow type : locale.types) typeNames.add(type.type);
            }
            List<String> types = new ArrayList<>(typeNames);
            Map<String, Integer> typeIds = new HashMap<>();
            for (int i = 0; i < types.size(); i++) typeIds.put(types.get(i), i);
            emitStringTable(writer, "rv_type_names", "rv_type_name_offsets", types);

            List<String> names = new ArrayList<>();
            List<int[]> localeRows = new ArrayList<>();
            List<int[]> typeRows = new ArrayList<>();
            for (LocaleRow locale : locales.values()) {
                names.add(locale.name);
                int first = typeRows.size();
                TypeRow defaultRow = locale.types.stream().filter(t -> t.type.isEmpty())
                        .findFirst().orElseThrow();
                for (TypeRow type : locale.types) {
                    if (!type.type.isEmpty()) {
                        typeRows.add(new int[]{typeIds.get(type.type), type.profile});
                    }
                }
                localeRows.add(new int[]{first, typeRows.size() - first,
                        locale.caseProfile, defaultRow.profile});
            }
            emitStringTable(writer, "rv_locale_names", "rv_locale_name_offsets", names);
            emitLocaleRows(writer, localeRows);
            emitTypeRows(writer, typeRows);
        }
    }

    private static void emitU16Array(BufferedWriter w, String name, int[] values)
            throws IOException {
        w.write("static const uint16_t " + name + "[] = {\n");
        emitNumbers(w, values, 12, value -> String.format(Locale.ROOT, "0x%04x", value));
        w.write("};\n\n");
    }

    private static void emitU32Array(BufferedWriter w, String name, int[] values)
            throws IOException {
        w.write("static const uint32_t " + name + "[] = {\n");
        int[] actual = values.length == 0 ? new int[]{0} : values;
        emitNumbers(w, actual, 8, value -> String.format(Locale.ROOT, "0x%08x", value));
        w.write("};\n");
        w.write("static const uint32_t " + name + "_count = " + values.length + ";\n\n");
    }

    private interface NumberFormatter { String format(int value); }

    private static void emitNumbers(BufferedWriter w, int[] values, int perLine,
                                    NumberFormatter formatter) throws IOException {
        for (int i = 0; i < values.length; i++) {
            if (i % perLine == 0) w.write("    ");
            w.write(formatter.format(values[i]));
            w.write(i + 1 == values.length ? "\n" : ", ");
            if (i % perLine == perLine - 1 && i + 1 != values.length) w.write("\n");
        }
    }

    private static void emitCaseMaps(BufferedWriter w, String name, List<CaseMap> maps)
            throws IOException {
        w.write("static const rv_case_map " + name + "[] = {\n");
        for (CaseMap map : maps) {
            w.write(String.format(Locale.ROOT, "    {0x%06x, 0x%06x, 0x%06x},\n",
                    map.codePoint, map.upper, map.lower));
        }
        w.write("};\nstatic const uint32_t " + name + "_count = " + maps.size() + ";\n\n");
    }

    private static void emitSequenceRows(BufferedWriter w, List<int[]> rows) throws IOException {
        w.write("static const rv_sequence_row rv_sequences[] = {\n");
        for (int[] row : rows) w.write("    {" + row[0] + ", " + row[1] + "},\n");
        w.write("};\nstatic const uint32_t rv_sequences_count = " + rows.size() + ";\n\n");
    }

    private static void emitPairs(BufferedWriter w, String name, Pair[] pairs)
            throws IOException {
        w.write("static const rv_pair " + name + "[] = {\n");
        if (pairs.length == 0) w.write("    {0, 0},\n");
        for (Pair pair : pairs) {
            w.write(String.format(Locale.ROOT, "    {0x%08x, 0x%08x},\n",
                    pair.element, pair.representative));
        }
        w.write("};\nstatic const uint32_t " + name + "_count = " + pairs.length + ";\n\n");
    }

    private static void emitProfileRows(BufferedWriter w, List<int[]> rows) throws IOException {
        w.write("static const rv_collation_profile rv_collation_profiles[] = {\n");
        for (int[] r : rows) {
            w.write(String.format(Locale.ROOT, "    {%d, %d, %d, %d, %d, %d},\n",
                    r[0], r[1], r[2], r[3], r[4], r[5]));
        }
        w.write("};\nstatic const uint16_t rv_collation_profiles_count = "
                + rows.size() + ";\n\n");
    }

    private static void emitStringTable(BufferedWriter w, String poolName, String offsetsName,
                                        List<String> strings) throws IOException {
        w.write("static const char " + poolName + "[] = {\n");
        List<Integer> offsets = new ArrayList<>();
        List<Integer> bytes = new ArrayList<>();
        int offset = 0;
        for (String string : strings) {
            offsets.add(offset);
            for (byte b : string.getBytes(StandardCharsets.US_ASCII)) {
                bytes.add(b & 0xff);
            }
            bytes.add(0);
            offset += string.length() + 1;
        }
        emitNumbers(w, bytes.stream().mapToInt(Integer::intValue).toArray(), 16,
                value -> String.format(Locale.ROOT, "0x%02x", value));
        w.write("};\n\n");
        emitU32Array(w, offsetsName, offsets.stream().mapToInt(Integer::intValue).toArray());
    }

    private static void emitLocaleRows(BufferedWriter w, List<int[]> rows) throws IOException {
        w.write("static const rv_locale_row rv_locales[] = {\n");
        for (int i = 0; i < rows.size(); i++) {
            int[] r = rows.get(i);
            w.write(String.format(Locale.ROOT, "    {%d, %d, %d, %d, %d},\n",
                    i, r[0], r[1], r[2], r[3]));
        }
        w.write("};\nstatic const uint16_t rv_locales_count = " + rows.size() + ";\n\n");
    }

    private static void emitTypeRows(BufferedWriter w, List<int[]> rows) throws IOException {
        w.write("static const rv_type_row rv_locale_types[] = {\n");
        for (int[] r : rows) w.write("    {" + r[0] + ", " + r[1] + "},\n");
        w.write("};\nstatic const uint32_t rv_locale_types_count = " + rows.size() + ";\n\n");
    }
}
