package main

import (
	"strings"
	"testing"
)

func TestVerifyLinuxDynamicRequirementsAllowsPureGoVivaBaseline(t *testing.T) {
	if err := verifyLinuxDynamicRequirements(
		"GoIM",
		[]string{"libc.so.6", "libdl.so.2", "libpthread.so.0"},
		[]string{"GLIBC_2.2.5", "GLIBC_2.3.4"},
	); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyLinuxDynamicRequirementsRejectsNewGLIBC(t *testing.T) {
	err := verifyLinuxDynamicRequirements("GoIM", []string{"libc.so.6"}, []string{"GLIBC_2.32", "GLIBC_2.34"})
	if err == nil || !strings.Contains(err.Error(), "GLIBC_2.32") || !strings.Contains(err.Error(), "GLIBC_2.34") {
		t.Fatalf("new GLIBC requirements error = %v", err)
	}
}

func TestVerifyLinuxDynamicRequirementsRejectsUnexpectedLibraries(t *testing.T) {
	err := verifyLinuxDynamicRequirements("GoIM", []string{"libc.so.6", "libgtk-3.so.0"}, nil)
	if err == nil || !strings.Contains(err.Error(), "libgtk-3.so.0") {
		t.Fatalf("unexpected library error = %v", err)
	}
}
