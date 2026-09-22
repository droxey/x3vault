package sim

import (
	"errors"
	"testing"
)

func TestParseDevicePath(t *testing.T) {
	ok, err := parseDevicePath("/ereader/wiki/index.md")
	if err != nil || ok != "/ereader/wiki/index.md" {
		t.Fatalf("got %q %v", ok, err)
	}
	dotdot, err := parseDevicePath("/ereader/../etc/passwd")
	if err != nil || dotdot != "/etc/passwd" {
		t.Fatalf("clean .. got %q %v", dotdot, err)
	}
	if _, err := parseDevicePath("/x\x00y"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("nul: %v", err)
	}
	if _, err := parseDevicePath(`\windows`); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("backslash: %v", err)
	}
}

func TestMkdirRejectsDotDotName(t *testing.T) {
	s := NewStore()
	if err := s.Mkdir("/", ".."); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("got %v", err)
	}
}
