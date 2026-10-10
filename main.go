package main

import (
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"regexp"
)

func main() {
	bin := flag.String("bin", "", "path to binary")
	fn := flag.String("fn", "main", "function(s) to decompile, comma-separated")
	all := flag.Bool("all", false, "decompile all detected functions")
	fnBatch := flag.Int("fn-batch", 1, "parallel workers when -all is set")
	fnGroup := flag.Int("fn-group", 1, "group N functions per LLM request")
	arch := flag.String("arch", "arm", "architecture: arm or x86")
	out := flag.String("out", "out.c", "output C file")
	maxIter := flag.Int("iter", 8, "max refinement iterations")
	stub := flag.Bool("stub", true, "auto-generate external stubs")
	difftest := flag.Bool("diff", false, "run differential test after compile")
	argsFlag := flag.String("args", "", "args for differential test (space separated)")
	cacheDir := flag.String("cache", ".llm2decomp-cache", "cache directory for decompiled functions")
	useCache := flag.Bool("use-cache", true, "reuse cached function results")
	skipLLM := flag.String("skip-llm", "", "comma-separated function names to skip LLM (pdf only)")
	flag.Parse()

	if *bin == "" {
		fmt.Fprintln(os.Stderr, "missing -bin")
		os.Exit(1)
	}
	absBin, err := filepath.Abs(*bin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "abs: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stat(absBin); err != nil {
		fmt.Fprintf(os.Stderr, "binary not found: %v\n", err)
		os.Exit(1)
	}

	if *fnBatch < 1 {
		*fnBatch = 1
	}
	if *fnGroup < 1 {
		*fnGroup = 1
	}

	skipSet := map[string]bool{}
	for _, s := range strings.Split(*skipLLM, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			skipSet[s] = true
		}
	}

	var fns []string
	if *all {
		detected, err := ListFunctions(absBin, *arch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "list funcs: %v\n", err)
			os.Exit(1)
		}
		if len(detected) == 0 {
			fmt.Fprintln(os.Stderr, "no functions detected")
			os.Exit(1)
		}
		fns = detected
	} else {
		for _, f := range strings.Split(*fn, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				fns = append(fns, f)
			}
		}
		if len(fns) == 0 {
			fns = []string{"main"}
		}
	}

	type group struct {
		names []string
		key   string
	}

	var groups []group
	for i := 0; i < len(fns); i += *fnGroup {
		end := i + *fnGroup
		if end > len(fns) {
			end = len(fns)
		}
		names := fns[i:end]
		groups = append(groups, group{
			names: names,
			key:   groupCacheKey(names),
		})
	}

	if *fnBatch > len(groups) {
		*fnBatch = len(groups)
	}

	fmt.Printf("total functions: %d (groups: %d, group size: %d, workers: %d)\n",
		len(fns), len(groups), *fnGroup, *fnBatch)

	if *useCache {
		if err := os.MkdirAll(*cacheDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "cache dir: %v\n", err)
			os.Exit(1)
		}
	}

	type result struct {
		key  string
		code string
		err  error
	}

	results := make(chan result, len(groups))
	sem := make(chan struct{}, *fnBatch)
	var wg sync.WaitGroup

	for _, g := range groups {
		wg.Add(1)
		go func(g group) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			key := strings.Join(g.names, ",")
			cachePath := filepath.Join(*cacheDir, "grp_"+g.key+".c")

			if *useCache {
				if data, err := os.ReadFile(cachePath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
					fmt.Printf("[cache] group %v\n", g.names)
					results <- result{key: key, code: string(data)}
					return
				}
			}

			allSkipped := true
			for _, n := range g.names {
				if !skipSet[n] {
					allSkipped = false
					break
				}
			}

			if allSkipped {
				fmt.Printf("[pdf ] group %v (skip LLM)\n", g.names)
				var sb strings.Builder
				for _, n := range g.names {
					asm, err := RizinDecompile(absBin, n, *arch)
					if err != nil {
						results <- result{key: key, err: err}
						return
					}
					fmt.Fprintf(&sb, "/* %s : raw pdf (LLM skipped) */\n%s\n\n", n, asm)
				}
				code := sb.String()
				if *useCache {
					_ = os.WriteFile(cachePath, []byte(code), 0644)
				}
				results <- result{key: key, code: code}
				return
			}

			fmt.Printf("[run ] group %v\n", g.names)
			code, err := decompileGroup(absBin, g.names, *arch, *maxIter, *stub)
			if err != nil {
				results <- result{key: key, err: err}
				return
			}
			if *useCache {
				_ = os.WriteFile(cachePath, []byte(code), 0644)
			}
			results <- result{key: key, code: code}
		}(g)
	}

	wg.Wait()
	close(results)

	byKey := map[string]string{}
	failed := []string{}
	for r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "fail %s: %v\n", r.key, r.err)
			failed = append(failed, r.key)
			continue
		}
		byKey[r.key] = r.code
	}

	ordered := make([]string, 0, len(groups))
	for _, g := range groups {
		key := strings.Join(g.names, ",")
		if c, ok := byKey[key]; ok && strings.TrimSpace(c) != "" {
			ordered = append(ordered, c)
		}
	}
	if len(ordered) == 0 {
		fmt.Fprintln(os.Stderr, "nothing decompiled")
		os.Exit(1)
	}

	merged := mergePieces(ordered)

	abs, _ := filepath.Abs(*out)
	if err := os.WriteFile(abs, []byte(merged), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d groups, %d failed)\n", abs, len(ordered), len(failed))

	if *difftest {
		fmt.Printf("[diff] running differential test\n")
		var extra []string
		if *argsFlag != "" {
			extra = strings.Fields(*argsFlag)
		}
		ok, report := DifferentialTest(absBin, abs, extra)
		fmt.Println(report)
		if !ok {
			os.Exit(2)
		}
	}
}

func groupCacheKey(names []string) string {
	h := sha1.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func decompileGroup(absBin string, fns []string, arch string, maxIter int, stub bool) (string, error) {
	var asmParts []string
	for _, fn := range fns {
		pseudo, err := RizinDecompile(absBin, fn, arch)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(pseudo) == "" {
			return "", fmt.Errorf("rizin returned empty output for %s", fn)
		}
		asmParts = append(asmParts, "/* === "+fn+" === */\n"+pseudo)
	}
	pseudoAll := strings.Join(asmParts, "\n\n")

	seed := Normalize(pseudoAll)
	if stub {
		seed = InjectStubs(seed)
	}

	var finalCode string
	var lastErr string

	for i := 1; i <= maxIter; i++ {
		t0 := time.Now()
		candidate, err := LLMRefine(fns, seed, lastErr)
		if err != nil {
			return "", err
		}
		_ = t0
		if stub {
			candidate = InjectStubs(candidate)
		}
		ok, cerr := TryCompile(candidate)
		if ok {
			finalCode = candidate
			break
		}
		lastErr = Truncate(cerr, 4000)
		seed = candidate
	}
	if finalCode == "" {
		finalCode = seed
	}
	return finalCode, nil
}

func decompileOne(absBin, fn, arch string, maxIter int, stub bool) (string, error) {
	return decompileGroup(absBin, []string{fn}, arch, maxIter, stub)
}

func sanitizeFilename(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "..", "_")
	return r.Replace(s)
}

func mergePieces(pieces []string) string {
	includes := []string{
		"#include <stdint.h>",
		"#include <stddef.h>",
		"#include <string.h>",
		"#include <stdlib.h>",
		"#include <stdio.h>",
		"#include <unistd.h>",
		"#include <fcntl.h>",
	}

	incSet := map[string]bool{}
	var bodyParts []string
	var externs []string
	externSet := map[string]bool{}

	for _, p := range pieces {
		lines := strings.Split(p, "\n")
		inStub := false
		for _, ln := range lines {
			t := strings.TrimSpace(ln)
			if t == "" {
				continue
			}
			if strings.HasPrefix(t, "/* auto-stubs */") {
				inStub = true
				continue
			}
			if strings.HasPrefix(t, "/* end auto-stubs */") {
				inStub = false
				continue
			}
			if strings.HasPrefix(t, "#include") {
				incSet[t] = true
				continue
			}
			if inStub || strings.HasPrefix(t, "extern ") {
				if !externSet[t] {
					externSet[t] = true
					externs = append(externs, t)
				}
				continue
			}
			bodyParts = append(bodyParts, ln)
		}
	}

	body := strings.Join(bodyParts, "\n")
	defined := collectDefinedNames(body)

	filteredExterns := externs[:0]
	for _, e := range externs {
		if name := externName(e); name != "" && defined[name] {
			continue
		}
		filteredExterns = append(filteredExterns, e)
	}
	externs = filteredExterns

	var sb strings.Builder
	emitted := map[string]bool{}
	for _, inc := range includes {
		if incSet[inc] {
			sb.WriteString(inc)
			sb.WriteString("\n")
			emitted[inc] = true
		}
	}
	for inc := range incSet {
		if !emitted[inc] {
			sb.WriteString(inc)
			sb.WriteString("\n")
		}
	}

	if len(externs) > 0 {
		sb.WriteString("\n/* auto-stubs */\n")
		for _, e := range externs {
			sb.WriteString(e)
			sb.WriteString("\n")
		}
		sb.WriteString("/* end auto-stubs */\n")
	}

	sb.WriteString("\n")
	sb.WriteString(body)
	sb.WriteString("\n")
	return sb.String()
}

func externName(decl string) string {
	s := strings.TrimSpace(decl)
	s = strings.TrimPrefix(s, "extern ")
	i := strings.IndexAny(s, "( ;")
	if i < 0 {
		return ""
	}
	head := strings.TrimSpace(s[:i])
	if head == "" {
		return ""
	}
	fields := strings.Fields(head)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func collectDefinedNames(body string) map[string]bool {
	re := regexp.MustCompile(`(?m)^\s*(?:[A-Za-z_][A-Za-z0-9_]*\s+)+([A-Za-z_][A-Za-z0-9_]*)\s*\([^;]*\)\s*\{`)
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out[m[1]] = true
	}
	return out
}
