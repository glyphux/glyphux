#![no_std]
#![no_main]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    core::arch::wasm32::unreachable()
}

#[link(wasm_import_module = "env")]
extern "C" {
    fn kv_get(key_ptr: *const u8, key_len: i32, out_ptr: *mut u8, out_cap: i32) -> i32;
    fn kv_set(key_ptr: *const u8, key_len: i32, val_ptr: *const u8, val_len: i32) -> i32;
}

static KEY: &[u8] = b"greeting";
static VAL: &[u8] = b"hello-from-guest";
static mut BUF: [u8; 64] = [0; 64];

/// Persistence-fixture write half: sets the fixed key and leaves it in
/// place (unlike kv_guest.rs's run_kv_roundtrip, which deletes at the
/// end) so a reloaded instance can read it back. Returns 0 on success,
/// -1 on host error.
#[no_mangle]
pub extern "C" fn persist_set() -> i32 {
    unsafe {
        let rc = kv_set(KEY.as_ptr(), KEY.len() as i32, VAL.as_ptr(), VAL.len() as i32);
        if rc != 0 {
            return -1;
        }
    }
    0
}

/// Persistence-fixture read half: reads the fixed key back and compares
/// against the expected value. Returns 1 on full success, a distinct
/// negative code identifying which step failed otherwise.
#[no_mangle]
pub extern "C" fn persist_get() -> i32 {
    unsafe {
        let n = kv_get(KEY.as_ptr(), KEY.len() as i32, BUF.as_mut_ptr(), BUF.len() as i32);
        if n == -1 {
            return -2; // not found
        }
        if n < 0 {
            return -3; // host error
        }
        if n != VAL.len() as i32 {
            return -4; // wrong length
        }
        let mut i = 0usize;
        while i < VAL.len() {
            if BUF[i] != VAL[i] {
                return -5; // wrong bytes
            }
            i += 1;
        }
    }
    1
}
