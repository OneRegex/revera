package conformance

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindRepositoryRoot walks up from start to the directory that holds the repository.
// It resolves symlinks first, so a linked checkout reports its real path.
func FindRepositoryRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	for candidate := abs; ; candidate = filepath.Dir(candidate) {
		if IsRepositoryRoot(candidate) {
			return candidate, nil
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
	}
	return "", fmt.Errorf("cannot find the re-vera2 repository from %s", start)
}

// IsRepositoryRoot reports whether root holds the component directories and the go1 module.
func IsRepositoryRoot(root string) bool {
	for _, rel := range []string{"go1", "go1/revera", "go1/probe", "rust1", "zig1", "cpp1"} {
		if ValidateDirectoryComponents(root, rel) != nil {
			return false
		}
	}
	modPath := filepath.Join(root, "go1", "go.mod")
	info, err := os.Lstat(modPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false
	}
	mod, err := os.ReadFile(modPath)
	if err != nil {
		return false
	}
	line, _, _ := bytes.Cut(mod, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	return bytes.Equal(line, []byte("module revera1"))
}

// ValidateDirectoryComponents checks that rel names a directory below root through plain directories only.
// A symlink or a file on the way is an error, so generated artifacts never land outside the tree.
func ValidateDirectoryComponents(root, rel string) error {
	rel = filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("path is not repository-relative: %s", filepath.ToSlash(rel))
	}
	current := root
	for _, component := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect directory %s: %w", filepath.ToSlash(rel), err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory path contains a symlink: %s", filepath.ToSlash(rel))
		}
		if !info.IsDir() {
			return fmt.Errorf("directory path contains a non-directory: %s", filepath.ToSlash(rel))
		}
	}
	return nil
}

// ResolveRepositoryRoot takes the -repo value of a command, or discovers the root from cwd when it is empty.
func ResolveRepositoryRoot(value, cwd string) (string, error) {
	if value == "" {
		return FindRepositoryRoot(cwd)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(cwd, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	if !IsRepositoryRoot(abs) {
		return "", fmt.Errorf("not a re-vera2 repository: %s", value)
	}
	return abs, nil
}
