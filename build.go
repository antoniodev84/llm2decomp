package main

import (
"bytes"
"fmt"
"os"
"os/exec"
"path/filepath"
"regexp"
"strings"
)

var undefRe = regexp.MustCompile(`\b(undefined\d*|undefined|code|uint|ulong|byte|word|dword|qword)\b`)

func Normalize(code string) string {
repl := map[string]string{
"undefined8": "uint64_t",
"undefined4": "uint32_t",
"undefined2": "uint16_t",
"undefined1": "uint8_t",
"undefined":  "uint64_t",
"code":       "void",
"uint":       "uint32_t",
"ulong":      "uint64_t",
"byte":       "uint8_t",
"word":       "uint16_t",
"dword":      "uint32_t",
"qword":      "uint64_t",
}
for k, v := range repl {
re := regexp.MustCompile(`\b` + regexp.QuoteMeta(k) + `\b`)
code = re.ReplaceAllString(code, v)
}
code = undefRe.ReplaceAllString(code, "uint64_t")
return code
}

func TryCompile(code string) (bool, string) {
dir, err := os.MkdirTemp("", "decomp-*")
if err != nil {
return false, err.Error()
}
defer os.RemoveAll(dir)

src := filepath.Join(dir, "unit.c")
if err := os.WriteFile(src, []byte(code), 0644); err != nil {
return false, err.Error()
}

cc := os.Getenv("CC")
if cc == "" {
cc = "gcc"
}

cmd := exec.Command(cc, "-std=c99", "-c", "-w", "-o", filepath.Join(dir, "unit.o"), src)
var stderr bytes.Buffer
cmd.Stderr = &stderr

if err := cmd.Run(); err != nil {
return false, stderr.String()
}
return true, ""
}

func Truncate(s string, n int) string {
s = strings.TrimSpace(s)
if len(s) <= n {
return s
}
return s[:n] + "\n...[truncated]"
}

func Fatalf(format string, args ...any) {
fmt.Fprintf(os.Stderr, format+"\n", args...)
os.Exit(1)
}
