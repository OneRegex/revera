package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTreeSourceReadsOnlyRegularFiles(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "data.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		rel   string
		setup func(native string) error
		want  string
	}{
		{"file symlink", "native/data.txt", func(native string) error {
			return os.Symlink(filepath.Join(outside, "data.txt"), filepath.Join(native, "data.txt"))
		}, "not a regular file"},
		{"directory symlink", "native/tests/data.txt", func(native string) error {
			target := filepath.Join(native, "untracked")
			if err := os.Mkdir(target, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(target, "data.txt"), []byte("untracked"), 0o644); err != nil {
				return err
			}
			return os.Symlink("untracked", filepath.Join(native, "tests"))
		}, "symlink"},
		// Opening a FIFO for reading would block until a writer shows up.
		{"FIFO", "native/data.txt", func(native string) error {
			return exec.Command("mkfifo", filepath.Join(native, "data.txt")).Run()
		}, "not a regular file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := t.TempDir()
			native := filepath.Join(repo, "native")
			if err := os.Mkdir(native, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := test.setup(native); err != nil {
				t.Skipf("cannot create the %s: %v", test.name, err)
			}
			root, err := os.OpenRoot(repo)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			src := &treeSource{root: root, tracked: map[string]bool{test.rel: true}}
			done := make(chan error, 1)
			go func() {
				_, _, err := src.read(test.rel)
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("read error = %v, want %q", err, test.want)
				}
			case <-time.After(time.Second):
				t.Fatal("read blocked")
			}
		})
	}
}

func TestTreeSourceStaysBoundToRepository(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitOrFail(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "native", "data.txt"), []byte("tracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOrFail(t, repo, "add", "native/data.txt")
	src, err := loadTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer src.root.Close()
	moved := filepath.Join(parent, "moved")
	replacement := filepath.Join(parent, "replacement")
	if err := os.MkdirAll(filepath.Join(replacement, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, "native", "data.txt"), []byte("untracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(replacement, repo); err != nil {
		t.Skipf("cannot replace the repository path with a symlink: %v", err)
	}
	data, _, err := src.read("native/data.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "tracked" {
		t.Fatalf("read %q from the replacement tree", data)
	}
}

func gitOrFail(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := git(repo, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A dirty build must archive the same files as a clean one.
func TestTreeAndCommitSourcesListTheSameFiles(t *testing.T) {
	repo := t.TempDir()
	gitOrFail(t, repo, "init", "-q")
	gitOrFail(t, repo, "config", "user.name", "Dist Test")
	gitOrFail(t, repo, "config", "user.email", "dist@example.invalid")
	if err := os.Mkdir(filepath.Join(repo, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "files", "regular"), []byte("regular"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("regular", filepath.Join(repo, "files", "link")); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	gitOrFail(t, repo, "add", "files")
	gitOrFail(t, repo, "commit", "-q", "-m", "base")
	head := gitOrFail(t, repo, "rev-parse", "HEAD")
	gitOrFail(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+head+",files/submodule")
	gitOrFail(t, repo, "commit", "-q", "-m", "submodule")

	tree, err := loadTree(repo)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := loadCommit(repo, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	treeFiles, err := tree.list("files")
	if err != nil {
		t.Fatal(err)
	}
	commitFiles, err := commit.list("files")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"files/regular"}
	if !slices.Equal(treeFiles, want) || !slices.Equal(commitFiles, want) {
		t.Fatalf("tree files = %q, commit files = %q, want %q", treeFiles, commitFiles, want)
	}
}

func TestLoadTreeRejectsUnmergedIndex(t *testing.T) {
	repo := t.TempDir()
	gitOrFail(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "file"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	oid := gitOrFail(t, repo, "hash-object", "-w", "file")
	entry := "100644 " + oid + " 1\tfile\n"
	if _, err := gitStdin(repo, strings.NewReader(entry), "update-index", "--index-info"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTree(repo); err == nil || !strings.Contains(err.Error(), "unmerged index entry") {
		t.Fatalf("loadTree error = %v, want an unmerged index error", err)
	}
}
