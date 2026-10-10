package main

import (
"flag"
"fmt"
"os"
"path/filepath"
"strings"
"sync"
"time"
)

func main() {
bin := flag.String("bin", "", "path to binary")
fn := flag.String("fn", "main", "function(s) to decompile, comma-separated")
all := flag.Bool("all", false, "decompile all detected functions")
fnBatch := flag.Int("fn-batch", 1, "parallel workers when -all is set")
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

if *fnBatch > len(fns) {
*fnBatch = len(fns)
}

fmt.Printf("total functions: %d (workers: %d)\n", len(fns), *fnBatch)

if *useCache {
if err := os.MkdirAll(*cacheDir, 0755); err != nil {
fmt.Fprintf(os.Stderr, "cache dir: %v\n", err)
os.Exit(1)
}
}

type result struct {
fn   string
code string
err  error
}

results := make(chan result, len(fns))
sem := make(chan struct{}, *fnBatch)
var wg sync.WaitGroup

for _, f := range fns {
wg.Add(1)
go func(name string) {
defer wg.Done()
sem <- struct{}{}
defer func() { <-sem }()

cachePath := filepath.Join(*cacheDir, sanitizeFilename(name)+".c")
if *useCache {
if data, err := os.ReadFile(cachePath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
fmt.Printf("[cache] %s\n", name)
results <- result{fn: name, code: string(data)}
return
}
}

if skipSet[name] {
fmt.Printf("[pdf ] %s (skip LLM)\n", name)
asm, err := RizinDecompile(absBin, name, *arch)
if err != nil {
results <- result{fn: name, err: err}
return
}
code := fmt.Sprintf("/* %s : raw pdf (LLM skipped) */\n%s\n", name, asm)
if *useCache {
_ = os.WriteFile(cachePath, []byte(code), 0644)
}
results <- result{fn: name, code: code}
return
}

fmt.Printf("[run ] %s\n", name)
code, err := decompileOne(absBin, name, *arch, *maxIter, *stub)
if err != nil {
results <- result{fn: name, err: err}
return
}
if *useCache {
_ = os.WriteFile(cachePath, []byte(code), 0644)
}
results <- result{fn: name, code: code}
}(f)
}

wg.Wait()
close(results)

ordered := make([]string, len(fns))
byFn := map[string]string{}
failed := []string{}
for r := range results {
if r.err != nil {
fmt.Fprintf(os.Stderr, "fail %s: %v\n", r.fn, r.err)
failed = append(failed, r.fn)
continue
}
byFn[r.fn] = r.code
}
for i, f := range fns {
ordered[i] = byFn[f]
}

var pieces []string
for _, p := range ordered {
if strings.TrimSpace(p) != "" {
pieces = append(pieces, p)
}
}
if len(pieces) == 0 {
fmt.Fprintln(os.Stderr, "nothing decompiled")
os.Exit(1)
}

merged := mergePieces(pieces)

abs, _ := filepath.Abs(*out)
if err := os.WriteFile(abs, []byte(merged), 0644); err != nil {
fmt.Fprintf(os.Stderr, "write: %v\n", err)
os.Exit(1)
}
fmt.Printf("wrote %s (%d functions, %d failed)\n", abs, len(pieces), len(failed))

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

func decompileOne(absBin, fn, arch string, maxIter int, stub bool) (string, error) {
pseudo, err := RizinDecompile(absBin, fn, arch)
if err != nil {
return "", err
}
if strings.TrimSpace(pseudo) == "" {
return "", fmt.Errorf("rizin returned empty output")
}

seed := Normalize(pseudo)
if stub {
seed = InjectStubs(seed)
}

var finalCode string
var lastErr string

for i := 1; i <= maxIter; i++ {
t0 := time.Now()
candidate, err := LLMRefine(seed, lastErr)
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
sb.WriteString(strings.Join(bodyParts, "\n"))
sb.WriteString("\n")
return sb.String()
}
