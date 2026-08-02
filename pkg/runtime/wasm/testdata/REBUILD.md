# Rebuilding the WASM test fixtures

Every `.wasm` file in this directory is the compiled output of the `.rs`
file with the same name (e.g. `busy_loop.wasm` ← `busy_loop.rs`). The
sources are `#![no_std]`/`#![no_main]` cdylibs with their own panic handler,
so they compile with a bare `rustc` — no Cargo manifest, no `-C
panic=unwind` support needed.

## Exact rebuild command

```sh
rustc --target wasm32-unknown-unknown -C opt-level=z -C panic=abort --crate-type cdylib -o busy_loop.wasm busy_loop.rs
```

`-C opt-level=z` keeps the fixtures small (size matters — they are committed
to the repo); `-C panic=abort` is required because `panic=unwind` is not
supported on wasm32-unknown-unknown.

## Byte-reproducibility

All six fixtures rebuild **byte-identical** (sha256 match, delta 0) under
rustc 1.97.0 (2d8144b78 2026-07-07) with the exact command above, so a
rebuilt fixture is a source-of-truth change only when the `.rs` source
changed. Byte-identity does however depend on the **exact rustc version**:
a different toolchain rewrites the name-section producer string and the
result no longer matches the committed bytes. If the toolchain is ever
upgraded, rebuild all six fixtures, verify the sha256 of every `.wasm` is
unchanged, and record the new rustc version here.
