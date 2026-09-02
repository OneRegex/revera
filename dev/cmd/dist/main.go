// Command dist stages the release assets: the Zig package archive, the native package archive,
// the two IR files, and a manifest with the source commit, the vegoc version, the IR digest and
// the checksum of every asset.
//
// Usage:
//
//	dist [-repo path] [-commit ref] [-allow-dirty] [-out dir]
//
// Every file comes out of the recorded commit, through git archive, so the manifest and the
// archives cannot disagree. A dirty tracked tree is refused unless -allow-dirty is given, in which
// case the working tree is read instead and the manifest says so.
// The archives carry no timestamps but the commit time, no ownership and no platform metadata,
// so two runs on one commit produce the same bytes.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oneregex/revera/dev/internal/conformance"
	"github.com/oneregex/revera/vego/compiler"
)

// licenseCopies are the package directories that carry their own copy of the root license files.
var licenseCopies = []string{"go", "rust", "zig", "native"}

// dataLicense is the license of the embedded CLDR data.
const dataLicense = "LICENSES/Unicode-3.0.txt"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("dist", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repoValue := flags.String("repo", "", "repository root (default: discover from the working directory)")
	commitRef := flags.String("commit", "HEAD", "the commit to build the assets from")
	allowDirty := flags.Bool("allow-dirty", false, "read the working tree instead of the commit when tracked files are modified")
	outDir := flags.String("out", "", "output directory (default tmp/dist below the repository root)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "dist does not accept positional arguments")
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	repo, err := conformance.ResolveRepositoryRoot(*repoValue, cwd)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *outDir == "" {
		*outDir = filepath.Join(repo, "tmp", "dist")
	}
	manifest, err := stage(repo, *commitRef, *allowDirty, *outDir)
	if err != nil {
		fmt.Fprintln(stderr, "dist:", err)
		return 1
	}
	fmt.Fprint(stdout, manifest)
	return 0
}

// source is one tree the assets come from: a commit or the working tree.
type source interface {
	// read returns the content of a file and whether it is executable.
	read(rel string) ([]byte, bool, error)
	// list returns every file below a directory, or the file itself, as slash paths.
	list(rel string) ([]string, error)
}

// commitSource reads a commit through git archive, so nothing outside the commit can leak in.
type commitSource struct {
	files map[string]tarEntry
}

type tarEntry struct {
	data []byte
	exec bool
}

func loadCommit(repo, commit string) (*commitSource, error) {
	out, err := git(repo, "archive", "--format=tar", commit)
	if err != nil {
		return nil, err
	}
	src := &commitSource{files: map[string]tarEntry{}}
	tr := tar.NewReader(bytes.NewReader(out))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read git archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s from git archive: %w", hdr.Name, err)
		}
		src.files[hdr.Name] = tarEntry{data: data, exec: hdr.Mode&0o111 != 0}
	}
	return src, nil
}

func (s *commitSource) read(rel string) ([]byte, bool, error) {
	entry, ok := s.files[rel]
	if !ok {
		return nil, false, fmt.Errorf("%s: not in the commit", rel)
	}
	return entry.data, entry.exec, nil
}

func (s *commitSource) list(rel string) ([]string, error) {
	if _, ok := s.files[rel]; ok {
		return []string{rel}, nil
	}
	var out []string
	for name := range s.files {
		if strings.HasPrefix(name, rel+"/") {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: not in the commit", rel)
	}
	sort.Strings(out)
	return out, nil
}

// treeSource reads the working tree; only the tracked files count, like git archive.
type treeSource struct {
	repo    string
	tracked map[string]bool
}

func loadTree(repo string) (*treeSource, error) {
	out, err := git(repo, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	src := &treeSource{repo: repo, tracked: map[string]bool{}}
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			src.tracked[name] = true
		}
	}
	return src, nil
}

func (s *treeSource) read(rel string) ([]byte, bool, error) {
	if !s.tracked[rel] {
		return nil, false, fmt.Errorf("%s: not tracked by git", rel)
	}
	full := filepath.Join(s.repo, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, false, err
	}
	return data, info.Mode()&0o111 != 0, nil
}

func (s *treeSource) list(rel string) ([]string, error) {
	if s.tracked[rel] {
		return []string{rel}, nil
	}
	var out []string
	for name := range s.tracked {
		if strings.HasPrefix(name, rel+"/") {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no tracked file below it", rel)
	}
	sort.Strings(out)
	return out, nil
}

func git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// stage builds every asset into outDir and returns the manifest text.
func stage(repo, commitRef string, allowDirty bool, outDir string) (string, error) {
	sha, err := git(repo, "rev-parse", "--verify", commitRef+"^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(sha))
	status, err := git(repo, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", err
	}
	dirty := len(bytes.TrimSpace(status)) != 0
	var src source
	commitLine := commit
	switch {
	case dirty && commitRef == "HEAD" && !allowDirty:
		return "", errors.New("tracked files are modified; commit them, name a commit with -commit, or pass -allow-dirty to stage the working tree")
	case dirty && commitRef == "HEAD":
		src, err = loadTree(repo)
		commitLine += " (dirty working tree)"
	default:
		src, err = loadCommit(repo, commit)
	}
	if err != nil {
		return "", err
	}
	stamp, err := git(repo, "log", "-1", "--format=%ct", commit)
	if err != nil {
		return "", err
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(stamp)), 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse the commit time: %w", err)
	}
	mtime := time.Unix(seconds, 0).UTC()

	version, err := packageVersion(src)
	if err != nil {
		return "", err
	}
	if err := checkLicenses(src); err != nil {
		return "", err
	}
	if err := checkChangelog(src, version); err != nil {
		return "", err
	}

	if err := os.RemoveAll(outDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	type asset struct {
		name string
		data []byte
	}
	var assets []asset

	zigPaths, err := zigPackagePaths(src)
	if err != nil {
		return "", err
	}
	zigArchive, err := archive(src, "revera-zig-"+version, "zig", zigPaths, mtime)
	if err != nil {
		return "", err
	}
	assets = append(assets, asset{"revera-zig-" + version + ".tar.gz", zigArchive})

	nativeFiles, err := src.list("native")
	if err != nil {
		return "", err
	}
	var nativePaths []string
	for _, name := range nativeFiles {
		if path.Base(name) == ".gitignore" {
			continue
		}
		nativePaths = append(nativePaths, strings.TrimPrefix(name, "native/"))
	}
	nativeArchive, err := archive(src, "revera-native-"+version, "native", nativePaths, mtime)
	if err != nil {
		return "", err
	}
	assets = append(assets, asset{"revera-native-" + version + ".tar.gz", nativeArchive})

	reveraIR, _, err := src.read("revera.vego.json")
	if err != nil {
		return "", err
	}
	probeIR, _, err := src.read("vego/probe/probe.vego.json")
	if err != nil {
		return "", err
	}
	assets = append(assets, asset{"revera.vego.json", reveraIR}, asset{"probe.vego.json", probeIR})

	var manifest strings.Builder
	fmt.Fprintf(&manifest, "revera %s\n", version)
	fmt.Fprintf(&manifest, "commit %s\n", commitLine)
	fmt.Fprintf(&manifest, "vegoc %s\n", compiler.Version)
	fmt.Fprintf(&manifest, "ir-digest sha256:%x\n", sha256.Sum256(reveraIR))
	for _, a := range assets {
		if err := os.WriteFile(filepath.Join(outDir, a.name), a.data, 0o644); err != nil {
			return "", err
		}
		fmt.Fprintf(&manifest, "sha256 %x %s\n", sha256.Sum256(a.data), a.name)
	}
	manifestPath := filepath.Join(outDir, "revera-"+version+".manifest")
	if err := os.WriteFile(manifestPath, []byte(manifest.String()), 0o644); err != nil {
		return "", err
	}
	return manifest.String(), nil
}

var (
	zigVersionPattern   = regexp.MustCompile(`(?m)^\s*\.version\s*=\s*"([^"]+)"`)
	cargoVersionPattern = regexp.MustCompile(`(?m)^version\s*=\s*"([^"]+)"`)
	cmakeVersionPattern = regexp.MustCompile(`(?s)project\(\s*revera\s.*?VERSION\s+([0-9][^\s)]*)`)
	zigPathsPattern     = regexp.MustCompile(`(?s)\.paths\s*=\s*\.\{(.*?)\}`)
	zigPathPattern      = regexp.MustCompile(`"([^"]*)"`)
	changelogPattern    = `(?m)^## %s - (\d{4}-\d{2}-\d{2})\s*$`
)

// packageVersion reads the version of the three package manifests and requires one value.
func packageVersion(src source) (string, error) {
	find := func(rel string, pattern *regexp.Regexp) (string, error) {
		data, _, err := src.read(rel)
		if err != nil {
			return "", err
		}
		m := pattern.FindSubmatch(data)
		if m == nil {
			return "", fmt.Errorf("%s: no version found", rel)
		}
		return string(m[1]), nil
	}
	zig, err := find("zig/build.zig.zon", zigVersionPattern)
	if err != nil {
		return "", err
	}
	cargo, err := find("rust/Cargo.toml", cargoVersionPattern)
	if err != nil {
		return "", err
	}
	cmake, err := find("native/CMakeLists.txt", cmakeVersionPattern)
	if err != nil {
		return "", err
	}
	if zig != cargo || zig != cmake {
		return "", fmt.Errorf("the package versions disagree: zig %s, rust %s, native %s", zig, cargo, cmake)
	}
	return zig, nil
}

// checkLicenses requires the root license files and identical copies in every package directory.
func checkLicenses(src source) error {
	root, _, err := src.read("LICENSE")
	if err != nil {
		return errors.New("no LICENSE at the repository root; choose the license before staging a release")
	}
	data, _, err := src.read(dataLicense)
	if err != nil {
		return err
	}
	for _, dir := range licenseCopies {
		for rel, want := range map[string][]byte{dir + "/LICENSE": root, dir + "/" + dataLicense: data} {
			got, _, err := src.read(rel)
			if err != nil {
				return fmt.Errorf("%w; run make licenses", err)
			}
			if !bytes.Equal(got, want) {
				return fmt.Errorf("%s differs from the root copy; run make licenses", rel)
			}
		}
	}
	return nil
}

// checkChangelog requires a dated section for the version.
func checkChangelog(src source, version string) error {
	data, _, err := src.read("CHANGELOG.md")
	if err != nil {
		return err
	}
	pattern := regexp.MustCompile(fmt.Sprintf(changelogPattern, regexp.QuoteMeta(version)))
	if !pattern.Match(data) {
		return fmt.Errorf("CHANGELOG.md has no dated section \"## %s - YYYY-MM-DD\"", version)
	}
	return nil
}

// zigPackagePaths reads the .paths list of the Zig manifest.
func zigPackagePaths(src source) ([]string, error) {
	data, _, err := src.read("zig/build.zig.zon")
	if err != nil {
		return nil, err
	}
	block := zigPathsPattern.FindSubmatch(data)
	if block == nil {
		return nil, errors.New("zig/build.zig.zon: no .paths list")
	}
	var paths []string
	for _, m := range zigPathPattern.FindAllSubmatch(block[1], -1) {
		paths = append(paths, string(m[1]))
	}
	if len(paths) == 0 {
		return nil, errors.New("zig/build.zig.zon: empty .paths list")
	}
	return paths, nil
}

// archive packs the named paths of dir below top into a deterministic gzip tar.
// A path that names a directory brings every file below it.
func archive(src source, top, dir string, paths []string, mtime time.Time) ([]byte, error) {
	files := map[string]tarEntry{}
	for _, rel := range paths {
		names, err := src.list(path.Join(dir, rel))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			data, exec, err := src.read(name)
			if err != nil {
				return nil, err
			}
			files[strings.TrimPrefix(name, dir+"/")] = tarEntry{data: data, exec: exec}
		}
	}
	dirs := map[string]bool{"": true}
	for name := range files {
		for d := path.Dir(name); d != "."; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	var entries []string
	for d := range dirs {
		entries = append(entries, d)
	}
	for name := range files {
		entries = append(entries, name)
	}
	sort.Strings(entries)

	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	// The gzip header carries neither a name nor a time.
	zw.Header = gzip.Header{OS: 255}
	tw := tar.NewWriter(zw)
	for _, name := range entries {
		full := top
		if name != "" {
			full = top + "/" + name
		}
		if dirs[name] {
			if err := tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir, Name: full + "/", Mode: 0o755, ModTime: mtime, Format: tar.FormatUSTAR,
			}); err != nil {
				return nil, err
			}
			continue
		}
		entry := files[name]
		mode := int64(0o644)
		if entry.exec {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg, Name: full, Mode: mode, Size: int64(len(entry.data)), ModTime: mtime, Format: tar.FormatUSTAR,
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(entry.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
