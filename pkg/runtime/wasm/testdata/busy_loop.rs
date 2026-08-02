#![no_std]
#![no_main]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    core::arch::wasm32::unreachable()
}

static mut SINK: u64 = 0;

/// Spins forever, accumulating into a static so the optimizer cannot
/// remove or trap-compile the loop (a side-effect-free `loop {}` is
/// legally compilable to nothing by LLVM — the store is the black-box
/// preventer). Deliberately never returns. The runtime's execution-timeout
/// / fuel deadline interrupts it via wazero's loop-header exit-code check
/// (WithCloseOnContextDone), which is the only way to terminate in-flight
/// guest execution in wazero v1.12.
#[no_mangle]
pub extern "C" fn spin_forever() -> i32 {
    let mut acc: u64 = 0;
    loop {
        acc = acc.wrapping_add(1);
        unsafe {
            SINK = acc;
        }
    }
}
