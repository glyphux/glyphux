#![no_std]
#![no_main]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    core::arch::wasm32::unreachable()
}

/// Grows linear memory one page (64 KiB) at a time until `target` pages
/// have been grown, trapping the moment memory.grow fails — i.e. the
/// runtime's memory cap (wazero WithMemoryLimitPages) is hit. Returns the
/// number of pages grown on success. A BOUNDED loop, not an infinite one:
/// on an engine with a large enough cap it completes normally, so the
/// test's red/green contrast (no error with a big cap, a cap error with a
/// small one) isolates the cap as the cause. memory.grow is an observable
/// side effect the optimizer cannot remove.
#[no_mangle]
pub extern "C" fn grow_to(target: i32) -> i32 {
    let mut grown: i32 = 0;
    while grown < target {
        // const MEM=0 (the only memory; legacy-const-generics promotion),
        // delta=1 page. Returns the previous size in pages, or usize::MAX
        // (-1) when the runtime memory cap (WithMemoryLimitPages) is hit.
        let prev = core::arch::wasm32::memory_grow::<0>(1);
        if prev == usize::MAX {
            // memory.grow returned -1: the runtime memory cap was hit.
            core::arch::wasm32::unreachable();
        }
        grown += 1;
    }
    grown
}
