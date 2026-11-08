//! The counting global allocator of the test binary.
//!
//! It forwards every call to the system allocator and counts two ways:
//!
//! - per thread, what the current thread asks for ([`counted`], [`watching`]),
//!   which tests running in parallel can use without disturbing each other;
//! - for the whole process, the live heap bytes and their peak ([`live`],
//!   [`reset_peak`], [`peak`]), which only mean something when one test runs
//!   alone (`--test-threads=1`), as the memory profile does.

use std::alloc::{GlobalAlloc, Layout, System};
use std::cell::Cell;
use std::sync::atomic::{AtomicUsize, Ordering};

/// Forwards to the system allocator and counts.
struct Counting;

/// How many allocation sizes [`watching`] can watch at once.
pub(super) const WATCHED: usize = 32;

thread_local! {
    static ALLOCATIONS: Cell<u64> = const { Cell::new(0) };
    static ALLOCATED_BYTES: Cell<u64> = const { Cell::new(0) };
    /// Sizes of a copy of a drawn series; 0 is unused.
    static WATCHED_SIZES: Cell<[usize; WATCHED]> = const { Cell::new([0; WATCHED]) };
    static WATCHED_HITS: Cell<u64> = const { Cell::new(0) };
}

/// Live heap bytes and their peak.
struct Heap {
    /// Bytes allocated and not yet freed.
    live: AtomicUsize,
    /// The highest `live` since the last reset.
    peak: AtomicUsize,
}

impl Heap {
    const fn new() -> Self {
        Heap {
            live: AtomicUsize::new(0),
            peak: AtomicUsize::new(0),
        }
    }

    fn grow(&self, bytes: usize) {
        let live = self.live.fetch_add(bytes, Ordering::Relaxed) + bytes;
        self.peak.fetch_max(live, Ordering::Relaxed);
    }

    fn shrink(&self, bytes: usize) {
        self.live.fetch_sub(bytes, Ordering::Relaxed);
    }

    /// A block resized from `old` to `new` bytes changes the live heap by
    /// the difference only: counting the new size before taking off the old
    /// one would record a peak of both that never existed.
    fn resize(&self, old: usize, new: usize) {
        if new >= old {
            self.grow(new - old);
        } else {
            self.shrink(old - new);
        }
    }

    fn live(&self) -> usize {
        self.live.load(Ordering::Relaxed)
    }

    fn reset_peak(&self) {
        self.peak.store(self.live(), Ordering::Relaxed);
    }

    fn peak(&self) -> usize {
        self.peak.load(Ordering::Relaxed)
    }
}

/// The heap of every thread of the process.
static HEAP: Heap = Heap::new();

fn record(bytes: usize) {
    // `try_with`: the thread-locals are gone while a thread shuts down.
    let _ = ALLOCATIONS.try_with(|count| count.set(count.get() + 1));
    let _ = ALLOCATED_BYTES.try_with(|total| total.set(total.get() + bytes as u64));
    if bytes > 0
        && WATCHED_SIZES
            .try_with(|sizes| sizes.get().contains(&bytes))
            .unwrap_or(false)
    {
        let _ = WATCHED_HITS.try_with(|hits| hits.set(hits.get() + 1));
    }
}

// SAFETY: every call is forwarded unchanged to `System`, which upholds the
// `GlobalAlloc` contract; counting touches only const-initialized
// thread-locals and atomics, which never allocate.
unsafe impl GlobalAlloc for Counting {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        record(layout.size());
        // SAFETY: the caller's guarantees for `layout` are passed on.
        let ptr = unsafe { System.alloc(layout) };
        if !ptr.is_null() {
            HEAP.grow(layout.size());
        }
        ptr
    }

    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        record(layout.size());
        // SAFETY: as for `alloc`.
        let ptr = unsafe { System.alloc_zeroed(layout) };
        if !ptr.is_null() {
            HEAP.grow(layout.size());
        }
        ptr
    }

    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        record(new_size);
        // SAFETY: `ptr` was allocated by this allocator, that is by `System`.
        let moved = unsafe { System.realloc(ptr, layout, new_size) };
        if !moved.is_null() {
            HEAP.resize(layout.size(), new_size);
        }
        moved
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        HEAP.shrink(layout.size());
        // SAFETY: `ptr` was allocated by this allocator, that is by `System`.
        unsafe { System.dealloc(ptr, layout) }
    }
}

#[global_allocator]
static ALLOCATOR: Counting = Counting;

/// Allocations and bytes requested by this thread while `f` runs.
pub(super) fn counted(f: impl FnOnce()) -> (u64, u64) {
    let before = (ALLOCATIONS.get(), ALLOCATED_BYTES.get());
    f();
    (
        ALLOCATIONS.get() - before.0,
        ALLOCATED_BYTES.get() - before.1,
    )
}

/// How many allocations of exactly one of `sizes` bytes this thread makes
/// while `f` runs.
pub(super) fn watching(sizes: &[usize], f: impl FnOnce()) -> u64 {
    let mut watched = [0; WATCHED];
    watched[..sizes.len()].copy_from_slice(sizes);
    WATCHED_SIZES.set(watched);
    let before = WATCHED_HITS.get();
    f();
    WATCHED_SIZES.set([0; WATCHED]);
    WATCHED_HITS.get() - before
}

/// Heap bytes the whole process holds now.
pub(super) fn live() -> usize {
    HEAP.live()
}

/// Starts a new peak at the bytes held now.
pub(super) fn reset_peak() {
    HEAP.reset_peak();
}

/// The most heap bytes the process held at once since [`reset_peak`].
pub(super) fn peak() -> usize {
    HEAP.peak()
}

#[cfg(test)]
mod tests {
    use super::Heap;

    /// A resize counts only the difference, so growing a block does not
    /// record a peak of its old and new size together.
    #[test]
    fn resizing_counts_only_the_difference() {
        let heap = Heap::new();
        heap.grow(100);
        heap.reset_peak();
        heap.resize(100, 160);
        assert_eq!((heap.live(), heap.peak()), (160, 160));
        heap.resize(160, 40);
        assert_eq!((heap.live(), heap.peak()), (40, 160));
        heap.resize(40, 40);
        heap.reset_peak();
        assert_eq!((heap.live(), heap.peak()), (40, 40));
        heap.grow(10);
        heap.shrink(50);
        assert_eq!((heap.live(), heap.peak()), (0, 50));
    }
}
