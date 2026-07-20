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
    fn kv_delete(key_ptr: *const u8, key_len: i32) -> i32;
}

static KEY: &[u8] = b"greeting";
static VAL: &[u8] = b"hello-from-guest";
static mut BUF: [u8; 64] = [0; 64];

/// Round-trips a value through the host KV store: set, get (compare), delete,
/// get again (expect not-found). Returns 1 on full success, a distinct
/// negative code identifying which step failed otherwise.
#[no_mangle]
pub extern "C" fn run_kv_roundtrip() -> i32 {
    unsafe {
        let set_rc = kv_set(KEY.as_ptr(), KEY.len() as i32, VAL.as_ptr(), VAL.len() as i32);
        if set_rc != 0 {
            return -1;
        }
        let n = kv_get(KEY.as_ptr(), KEY.len() as i32, BUF.as_mut_ptr(), BUF.len() as i32);
        if n != VAL.len() as i32 {
            return -2;
        }
        let mut i = 0usize;
        while i < VAL.len() {
            if BUF[i] != VAL[i] {
                return -3;
            }
            i += 1;
        }
        let del_rc = kv_delete(KEY.as_ptr(), KEY.len() as i32);
        if del_rc != 0 {
            return -4;
        }
        let n2 = kv_get(KEY.as_ptr(), KEY.len() as i32, BUF.as_mut_ptr(), BUF.len() as i32);
        if n2 != -1 {
            return -5;
        }
    }
    1
}

/// Deliberately traps via an out-of-bounds write to a wild pointer, to prove
/// the host survives a guest trap (wazero isolates each module's own linear
/// memory / call stack; a trap must not crash the host process or corrupt a
/// sibling instance).
#[no_mangle]
pub extern "C" fn trigger_trap() -> i32 {
    unsafe {
        let p = 0xffff_fff0u32 as *mut u8;
        *p = 1;
    }
    0
}
