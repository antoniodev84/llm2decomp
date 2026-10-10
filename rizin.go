package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func RizinDecompile(bin, fn, arch string) (string, error) {
	if _, err := exec.LookPath("rizin"); err != nil {
		return "", fmt.Errorf("rizin not found in PATH")
	}

	archFlag := "arm"
	if strings.HasPrefix(arch, "x86") || arch == "x64" || arch == "amd64" {
		archFlag = "x86"
	}

	script := strings.Join([]string{
		"e scr.color=false",
		fmt.Sprintf("e asm.arch=%s", archFlag),
		"aaa",
		"aac",
		fmt.Sprintf("s sym.%s", fn),
		fmt.Sprintf("pdf @ sym.%s", fn),
	}, ";")

	cmd := exec.Command("rizin", "-q", "-c", script, bin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rizin run: %v: %s", err, stderr.String())
	}

	out := stdout.String()
	if strings.TrimSpace(out) == "" {
		out = stderr.String()
	}
	return out, nil
}
func ListFunctions(bin, arch string) ([]string, error) {
	archFlag := "arm"
	if strings.HasPrefix(arch, "x86") || arch == "x64" || arch == "amd64" {
		archFlag = "x86"
	}

	script := strings.Join([]string{
		"e scr.color=false",
		fmt.Sprintf("e asm.arch=%s", archFlag),
		"aaa",
		"afl",
	}, ";")

	cmd := exec.Command("rizin", "-q", "-c", script, bin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rizin list: %v: %s", err, stderr.String())
	}

	var fns []string
	seen := map[string]bool{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		name := fields[len(fields)-1]
		if strings.HasPrefix(name, "sym.") {
			name = strings.TrimPrefix(name, "sym.")
		}
		if name == "" || seen[name] {
			continue
		}
		if strings.HasPrefix(name, "loc.") || strings.HasPrefix(name, "fcn.") {
			continue
		}
		seen[name] = true
		fns = append(fns, name)
	}
	return fns, nil
}
