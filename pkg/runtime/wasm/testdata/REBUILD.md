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

## Byte-reproducibility caveat

Rebuilds are only byte-identical when the **exact same rustc version** that
produced the committed `.wasm` is used. The committed fixtures were built
with an older toolchain whose version string is not recorded; rebuilding
them with rustc 1.97.0 (2d8144b78 2026-07-07, the version this was verified
against) produces a functionally identical module that differs ONLY in the
name-section producer string (a consistent +8-byte delta across all six
fixtures). If byte-reproducible commits ever matter, pick one rustc,
rebuild all six fixtures, and record that version here — until then, treat
a rebuild as a source-of-truth change only when the `.rs` source changed.
