use revera::{ErrorKind, Locale, Regex, RegexBuilder};

#[test]
fn find_and_captures() {
    let re = Regex::new("([a-z]+)([0-9]*)").unwrap();
    assert_eq!(re.captures_len(), 3);
    assert!(re.is_match("__abc12__").unwrap());

    let m = re.find("__abc12__").unwrap().unwrap();
    assert_eq!(m.range(), 2..7);
    assert_eq!(m.as_str(), "abc12");
    assert_eq!(m.len(), 5);
    assert!(!m.is_empty());

    let caps = re.captures("__abc12__").unwrap().unwrap();
    assert_eq!(caps.len(), 3);
    assert_eq!(&caps[0], "abc12");
    assert_eq!(&caps[1], "abc");
    assert_eq!(&caps[2], "12");
    assert_eq!(caps.get(1).unwrap().start(), 2);
}

#[test]
fn match_text_uses_utf8_byte_boundaries() {
    let re = Regex::new("(é)(β)").unwrap();
    let subject = "xéβz";
    let m = re.find(subject).unwrap().unwrap();
    assert_eq!(m.range(), 1..5);
    assert_eq!(m.len(), 4);
    assert_eq!(m.as_str(), "éβ");

    let caps = re.captures(subject).unwrap().unwrap();
    assert_eq!(caps.get(1).unwrap().range(), 1..3);
    assert_eq!(caps.get(1).unwrap().as_str(), "é");
    assert_eq!(caps.get(2).unwrap().range(), 3..5);
    assert_eq!(caps.get(2).unwrap().as_str(), "β");
}

#[test]
fn capture_solver_wraps_hash_arithmetic() {
    let re = Regex::new("(a*)*").unwrap();
    let caps = re.captures("").unwrap().unwrap();
    assert_eq!(caps.get(0).unwrap().as_str(), "");
    assert_eq!(caps.get(1).unwrap().as_str(), "");
}

#[test]
fn absent_group_reads_as_none() {
    let re = Regex::new("(a)|(b)").unwrap();
    let caps = re.captures("a").unwrap().unwrap();
    assert!(caps.get(1).is_some());
    assert!(caps.get(2).is_none());
    assert!(caps.get(9).is_none());
    let seen: Vec<_> = caps.iter().map(|g| g.map(|m| m.as_str())).collect();
    assert_eq!(seen, vec![Some("a"), Some("a"), None]);
}

#[test]
fn no_match_is_none() {
    let re = Regex::new("z+").unwrap();
    assert!(!re.is_match("abc").unwrap());
    assert!(re.find("abc").unwrap().is_none());
    assert!(re.captures("abc").unwrap().is_none());
    assert_eq!(re.find_iter("abc").count(), 0);
}

#[test]
fn iterators_walk_every_match() {
    let re = Regex::new("(a+)(b*)").unwrap();
    let found: Vec<&str> = re
        .find_iter("aab a aabbb")
        .map(|m| m.unwrap().as_str())
        .collect();
    assert_eq!(found, vec!["aab", "a", "aabbb"]);

    let groups: Vec<String> = re
        .captures_iter("aab a")
        .map(|c| {
            let c = c.unwrap();
            format!("{}|{}", &c[1], &c[2])
        })
        .collect();
    assert_eq!(groups, vec!["aa|b".to_string(), "a|".to_string()]);
}

#[test]
fn replacement() {
    let re = Regex::new("(a+)(b*)").unwrap();
    assert_eq!(re.replace_all("xaabyy", "[&:\\2]").unwrap(), "x[aab:b]yy");
    assert_eq!(re.replacen("aa bb aa", 1, "X").unwrap(), "X bb aa");
    let out = re
        .replace_all_with("xaabyy", |caps| caps[1].to_uppercase())
        .unwrap();
    assert_eq!(out, "xAAyy");
    assert_eq!(re.replace_all_with("xyz", |_| String::new()).unwrap(), "xyz");
}

#[test]
fn builder_options() {
    let re = RegexBuilder::new("ab+").case_insensitive(true).build().unwrap();
    assert!(re.is_match("ABBB").unwrap());

    let re = RegexBuilder::new("^b").newline_sensitive(true).build().unwrap();
    assert_eq!(re.find("a\nbc").unwrap().unwrap().range(), 2..3);

    let re = RegexBuilder::new("a+").shortest_match(true).build().unwrap();
    assert_eq!(re.find("aaa").unwrap().unwrap().range(), 0..1);

    let re = RegexBuilder::new("a+").no_captures(true).build().unwrap();
    assert!(re.is_match("baa").unwrap());
    assert_eq!(re.find("baa").unwrap_err().kind(), ErrorKind::NoCaptures);
}

#[test]
fn locales() {
    let cs = Locale::open("cs", "").expect("cs locale");
    let re = RegexBuilder::new("[[.ch.]]").locale(cs).build().unwrap();
    assert!(re.is_match("ch").unwrap());
    assert!(Locale::open("xx-not-there", "").is_none());

    let names = Locale::names();
    assert!(names.len() > 1000);
    assert!(names.iter().any(|n| n == "cs"));
}

#[test]
fn errors_carry_a_kind_and_a_position() {
    let err = Regex::new("a(").unwrap_err();
    assert_eq!(err.kind(), ErrorKind::Pattern);
    assert_eq!(err.offset(), Some(2));
    assert_eq!(err.to_string(), "invalid regular expression at byte 2");

    let err = Regex::new("[[:bogus:]]").unwrap_err();
    assert_eq!(err.kind(), ErrorKind::CharacterClass);
}

#[test]
fn contract_grows_with_the_input_bound() {
    let re = Regex::new("(a|ab)(c|bcd)(d*)").unwrap();
    let big = re.contract(1 << 12);
    assert_eq!(big.max_input, 1 << 12);
    assert!(big.heap_bytes > 0 && big.stack_bytes > 0 && big.steps > 0);
    assert!(big.one_pass.is_none());
    assert!(big.solver.is_some());
    assert!(big.matcher.steps > 0);
    assert!(re.contract(16).steps < big.steps);
    // An absurd bound clamps to the subject limit of the engine.
    assert_eq!(re.contract(usize::MAX).max_input, (1 << 31) - 1);
}

#[test]
fn contract_selects_reachable_backends() {
    let contract = Regex::new("a*").unwrap().contract(64);
    assert!(contract.one_pass.is_none());
    assert!(contract.solver.is_none());
    assert_eq!(contract.heap_bytes, contract.matcher.heap_bytes);
    assert_eq!(contract.stack_bytes, contract.matcher.stack_bytes);
    assert_eq!(contract.steps, contract.matcher.steps);

    let grouped = Regex::new("(abc+)").unwrap().contract(1000);
    assert!(grouped.one_pass.is_some());
    assert!(grouped.solver.is_none());
    assert_eq!(grouped.heap_bytes, 37_757);
    assert_eq!(grouped.stack_bytes, 6_144);
    assert_eq!(grouped.steps, 937_980);
}

#[test]
fn one_regex_serves_several_threads() {
    let re = Regex::new("[0-9]+").unwrap();
    std::thread::scope(|s| {
        for _ in 0..4 {
            s.spawn(|| {
                for _ in 0..200 {
                    assert_eq!(re.find("ab 1234 cd").unwrap().unwrap().as_str(), "1234");
                }
            });
        }
    });
}
