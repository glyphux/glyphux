#![no_std]
#![no_main]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    core::arch::wasm32::unreachable()
}

/// Burns a bounded but heavy amount of pure compute: for i in 0..n it
/// accumulates acc = acc + i*i, pushing every intermediate acc through
/// core::hint::black_box — an opaque-to-the-optimizer barrier that makes
/// each iteration's result observable, so LLVM can neither remove the loop
/// nor collapse it into the closed-form sum of squares (rustc 1.97
/// -C opt-level=z eliminated a pure `acc += i*i` loop — and even one with
/// a store to an internal-linkage static — down to `return 0`). Unlike
/// spin_forever (busy_loop.rs) this TERMINATES: given a large enough
/// budget it finishes; with a small fuel budget the runtime's fuel
/// deadline interrupts it mid-loop and the call errors with
/// wasm.ErrFuelExhausted. The caller picks n, so the test controls how
/// long a full burn takes.
#[no_mangle]
pub extern "C" fn fuel_burn(n: i32) -> i64 {
    let mut acc: u64 = 0;
    let mut i: u64 = 0;
    while i < n as u64 {
        acc = core::hint::black_box(acc).wrapping_add(i.wrapping_mul(i));
        i += 1;
    }
    core::hint::black_box(acc) as i64
}
