#![no_std]
#![no_main]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    core::arch::wasm32::unreachable()
}

#[link(wasm_import_module = "env")]
extern "C" {
    fn emit_event(name_ptr: *const u8, name_len: i32, payload_ptr: *const u8, payload_len: i32) -> i32;
}

static NAME: &[u8] = b"guest.ping";
static PAYLOAD: &[u8] = b"pong";

/// Emits a single event through the host's event bridge. This module's ONLY
/// import is emit_event — it deliberately does not import any kv_* function,
/// so it can be instantiated on its own to prove the events-domain host
/// function set is what gates it, independent of the KV domain.
#[no_mangle]
pub extern "C" fn run_emit() -> i32 {
    unsafe { emit_event(NAME.as_ptr(), NAME.len() as i32, PAYLOAD.as_ptr(), PAYLOAD.len() as i32) }
}

// --- Subscribe-side (host -> guest callback) ---
//
// event_buf_ptr/on_event are the two exports Runtime.Subscribe (runtime.go)
// looks for: the host writes an emitted event's payload bytes into the
// buffer event_buf_ptr() points at, then calls on_event(len) so the guest
// observes it. last_event_len/last_event_byte exist purely so a test can
// read back what the guest received without needing its own separate host
// import.

static mut EVENT_BUF: [u8; 128] = [0; 128];
static mut LAST_EVENT_LEN: i32 = -1;

#[no_mangle]
pub extern "C" fn event_buf_ptr() -> i32 {
    unsafe {
        let p = core::ptr::addr_of_mut!(EVENT_BUF) as *mut u8;
        p as i32
    }
}

#[no_mangle]
pub extern "C" fn on_event(len: i32) -> i32 {
    unsafe {
        LAST_EVENT_LEN = len;
    }
    len
}

#[no_mangle]
pub extern "C" fn last_event_len() -> i32 {
    unsafe { LAST_EVENT_LEN }
}

#[no_mangle]
pub extern "C" fn last_event_byte(i: i32) -> i32 {
    unsafe {
        let buf = core::ptr::addr_of!(EVENT_BUF) as *const u8;
        *buf.add(i as usize) as i32
    }
}
