package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func DifferentialTest(origBin, cFile string, args []string) (bool, string) {
	var b strings.Builder

	origOut, origCode, err := runBinary(origBin, args)
	if err != nil {
		fmt.Fprintf(&b, "orig run failed: %v\n", err)
		return false, b.String()
	}

	tmp, err := os.MkdirTemp("", "diff-*")
	if err != nil {
		fmt.Fprintf(&b, "tmpdir: %v\n", err)
		return false, b.String()
	}
	defer os.RemoveAll(tmp)

	cc := os.Getenv("CC")
	if cc == "" {
		cc = "gcc"
	}

	exe := filepath.Join(tmp, "rebuilt")
	cmd := exec.Command(cc, "-std=c99", "-w", "-o", exe, cFile)
	var cerr bytes.Buffer
	cmd.Stderr = &cerr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(&b, "link failed: %v\n%s\n", err, cerr.String())
		return false, b.String()
	}

	newOut, newCode, err := runBinary(exe, args)
	if err != nil {
		fmt.Fprintf(&b, "rebuilt run failed: %v\n", err)
		return false, b.String()
	}

	ok := true
	if origCode != newCode {
		ok = false
		fmt.Fprintf(&b, "exit code mismatch: orig=%d new=%d\n", origCode, newCode)
	}
	if origOut != newOut {
		ok = false
		fmt.Fprintf(&b, "stdout mismatch\n--- orig ---\n%s\n--- new ---\n%s\n", origOut, newOut)
	}

	if ok {
		fmt.Fprintf(&b, "differential test PASSED (exit=%d, stdout=%d bytes)\n", origCode, len(origOut))
	}
	return ok, b.String()
}

func runBinary(path string, args []string) (string, int, error) {
	cmd := exec.Command(path, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stdout

	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			return "", -1, err
		}
	}
	return stdout.String(), code, nil
}
