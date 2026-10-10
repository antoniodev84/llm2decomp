# llm2decomp

LLM-assisted decompiler: takes an ELF binary, extracts a function with
rizin (`pdf`), then asks an LLM to translate the assembly into valid C99.

## Requirements

- `rizin` in PATH
- `gcc`
- An OpenAI-compatible LLM endpoint

## Build

    go build -o llm2decomp .

## Usage

    export LLM_BASE_URL=https://api.groq.com/openai/v1
    export LLM_API_KEY=gsk_...
    export LLM_MODEL=openai/gpt-oss-120b

    ./llm2decomp --bin ./demo.elf --fn main
    ./llm2decomp --bin ./demo.elf --all --fn-batch 4

Output goes to `out.c`. Flags: `-arch`, `-iter`, `-out`, `-diff`,
`-skip-llm`, `-cache`, `-use-cache`.
## Examples

    ./llm2decomp --bin tests/demo.elf --fn main
    ./llm2decomp --bin tests/demo.elf --fn main,factorial
    ./llm2decomp --bin tests/demo.elf --all --fn-batch 4
    ./llm2decomp --bin tests/demo.elf --all --skip-llm main,factorial
## License
MIT
