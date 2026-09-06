package storage

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSafeJoinOK(t *testing.T) {
	root := t.TempDir()
	got, err := SafeJoin(root, "u1/20260102", "f1.jpg")
	if err != nil {
		t.Fatalf("want ok, got %v", err)
	}
	want, _ := filepath.Abs(filepath.Join(root, "u1", "20260102", "f1.jpg"))
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"../escape", "a/../../b", "u1/../../victim", ".."} {
		if _, err := SafeJoin(root, p); !errors.Is(err, ErrPathInvalid) {
			t.Fatalf("SafeJoin(%q) want ErrPathInvalid, got %v", p, err)
		}
	}
}

func TestValidSuffix(t *testing.T) {
	for _, s := range []string{"jpg", "tar.gz", "PNG", "a1.b2"} {
		if !ValidSuffix(s) {
			t.Fatalf("ValidSuffix(%q) want true", s)
		}
	}
	for _, s := range []string{"", "abcdefghi", "a/b", "..", "a b", ".jpg"} {
		if ValidSuffix(s) {
			t.Fatalf("ValidSuffix(%q) want false", s)
		}
	}
}

func TestValidStorageID(t *testing.T) {
	for _, s := range []string{"1", "340031688262287360"} {
		if !ValidStorageID(s) {
			t.Fatalf("ValidStorageID(%q) want true", s)
		}
	}
	for _, s := range []string{"", "abc", "../x", "123456789012345678901234567890123"} {
		if ValidStorageID(s) {
			t.Fatalf("ValidStorageID(%q) want false", s)
		}
	}
}
