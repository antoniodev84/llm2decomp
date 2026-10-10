package main

import (
	"regexp"
	"sort"
	"strings"
)

var knownLibc = map[string]string{
	"printf":   "int printf(const char *, ...);",
	"fprintf":  "int fprintf(void *, const char *, ...);",
	"sprintf":  "int sprintf(char *, const char *, ...);",
	"snprintf": "int snprintf(char *, size_t, const char *, ...);",
	"puts":     "int puts(const char *);",
	"putchar":  "int putchar(int);",
	"malloc":   "void *malloc(size_t);",
	"calloc":   "void *calloc(size_t, size_t);",
	"realloc":  "void *realloc(void *, size_t);",
	"free":     "void free(void *);",
	"memcpy":   "void *memcpy(void *, const void *, size_t);",
	"memset":   "void *memset(void *, int, size_t);",
	"memmove":  "void *memmove(void *, const void *, size_t);",
	"memcmp":   "int memcmp(const void *, const void *, size_t);",
	"strlen":   "size_t strlen(const char *);",
	"strcmp":   "int strcmp(const char *, const char *);",
	"strncmp":  "int strncmp(const char *, const char *, size_t);",
	"strcpy":   "char *strcpy(char *, const char *);",
	"strncpy":  "char *strncpy(char *, const char *, size_t);",
	"strcat":   "char *strcat(char *, const char *);",
	"atoi":     "int atoi(const char *);",
	"atol":     "long atol(const char *);",
	"exit":     "void exit(int);",
	"abort":    "void abort(void);",
	"read":     "long read(int, void *, size_t);",
	"write":    "long write(int, const void *, size_t);",
	"open":     "int open(const char *, int, ...);",
	"close":    "int close(int);",
	"fopen":    "void *fopen(const char *, const char *);",
	"fclose":   "int fclose(void *);",
	"fread":    "size_t fread(void *, size_t, size_t, void *);",
	"fwrite":   "size_t fwrite(const void *, size_t, size_t, void *);",
	"getenv":   "char *getenv(const char *);",
	"rand":     "int rand(void);",
	"srand":    "void srand(unsigned);",
	"time":     "long time(long *);",
	"usleep":   "int usleep(unsigned);",
	"sleep":    "unsigned sleep(unsigned);",
}

var callRe = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

var keywords = map[string]bool{
	"if": true, "while": true, "for": true, "switch": true,
	"return": true, "sizeof": true, "do": true, "else": true,
	"case": true, "break": true, "continue": true, "goto": true,
	"defined": true,
}

func CollectCalls(code string) []string {
	seen := map[string]bool{}
	for _, m := range callRe.FindAllStringSubmatch(code, -1) {
		name := m[1]
		if keywords[name] {
			continue
		}
		if isDefinedLocally(code, name) {
			continue
		}
		if isTypeName(name) {
			continue
		}
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isDefinedLocally(code, name string) bool {
	pat := regexp.MustCompile(`(?m)^\s*(?:[A-Za-z_][A-Za-z0-9_]*\s+)+` + regexp.QuoteMeta(name) + `\s*\(`)
	return pat.FindStringIndex(code) != nil
}

func isTypeName(name string) bool {
	types := map[string]bool{
		"uint8_t": true, "uint16_t": true, "uint32_t": true, "uint64_t": true,
		"int8_t": true, "int16_t": true, "int32_t": true, "int64_t": true,
		"void": true, "int": true, "char": true, "long": true, "short": true,
		"float": true, "double": true, "unsigned": true, "signed": true,
		"size_t": true, "ssize_t": true, "bool": true,
	}
	return types[name]
}

func InjectStubs(code string) string {
	calls := CollectCalls(code)
	if len(calls) == 0 {
		return code
	}

	var stubs []string
	for _, c := range calls {
		if decl, ok := knownLibc[c]; ok {
			stubs = append(stubs, decl)
			continue
		}
		if strings.HasPrefix(c, "__") {
			continue
		}
		stubs = append(stubs, "extern uint64_t "+c+"();")
	}

	if len(stubs) == 0 {
		return code
	}

	block := "\n/* auto-stubs */\n" + strings.Join(stubs, "\n") + "\n/* end auto-stubs */\n\n"

	marker := "/* end auto-stubs */"
	if idx := strings.Index(code, marker); idx >= 0 {
		code = code[idx+len(marker):]
	}

	if i := strings.Index(code, "\n\n"); i > 0 && strings.Contains(code[:i], "#include") {
		return code[:i+1] + block + code[i+1:]
	}
	return block + code
}
