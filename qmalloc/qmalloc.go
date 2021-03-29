// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

// Package qmalloc provides a fast and simple malloc library.
package qmalloc

import (
	//	"math/bits"
	"reflect"
	"sync"
	"unsafe"
)

const NAME = "qmalloc"

// size we round to, must be 2^n and
// sizeof(qmFrag) +sizeof(qmFragEnd) must be a multiple of ROUNDTO
const (
	RoundTo     = 16
	RoundToMask = ^(uint64(RoundTo) - 1)
)

const MinFragSize = RoundTo

var BuildTags []string

// MUsed contains the qmalloc memory usage statistics.
type MUsed struct {
	Used        uint64 // total size allocated
	RealUsed    uint64 // real size = Used + malloc overhead
	MaxRealUsed uint64
}

// Options encodes various configuration flags for QMalloc
type Options uint32

const (
	QMDebug          Options = 1 << iota
	QMChecks                 // check each fragment for overflow
	QMJoinFree               // join free fragments on free (expensive)
	QMFragAvoidance          // try harder to avoid fragmentation
	QMDumpStatsShort         // dump status in log, short version
	QMDefaultOptions = QMChecks | QMFragAvoidance
)

// QMalloc is the memory block or arena used for allocating.
// It includes the actual memory area used, all the bookkeeping information
// and the classical malloc functions (as methods).
type QMalloc struct {
	optSize   uint32 // optimize for allocs < optSize
	optFactor uint32 // optimize factor = round log2(optSize)
	options   Options
	hashSize  uint32
	size      uint64 // total size
	used      MUsed  // statistics

	firstFrag   *qmFrag
	lastFragEnd *qmFragEnd

	bigLock sync.Mutex

	freeH []qmFragLst // free fragments lists
	mem   []byte      // actual memory used
}

// Debug returns true if malloc debugging is turned on.
func (qm *QMalloc) Debug() bool { return qm.options&QMDebug != 0 }

// Debug returns true if malloc boundary checking is turned on.
func (qm *QMalloc) BChecks() bool { return qm.options&QMChecks != 0 }

// Debug returns true if malloc fragmentation avoidance is turned on.
func (qm *QMalloc) FragAvoidance() bool {
	return qm.options&QMFragAvoidance != 0
}

// Debug returns true if malloc fragmentation avoidance is turned on.
func (qm *QMalloc) JoinFree() bool {
	return qm.options&QMJoinFree != 0
}

func (qm *QMalloc) lock() {
	qm.bigLock.Lock()
}
func (qm *QMalloc) unlock() {
	qm.bigLock.Unlock()
}

// addUsed increases the "used" stats with the give size.
// size should be a freed fragment size.
func (qm *QMalloc) addUsed(size uint64) {
	qm.used.Used += size
	qm.used.RealUsed += size
	if qm.used.MaxRealUsed < qm.used.RealUsed {
		qm.used.MaxRealUsed = qm.used.RealUsed
	}
}

// subUsed subtracts size from the "used" used
func (qm *QMalloc) subUsed(size uint64) {
	qm.used.Used -= size
	qm.used.RealUsed -= size
}

// addOverhead adds a fragment overhead to the internal
// bookkeeping used.
func (qm *QMalloc) addOverhead(fOverhead uintptr) {
	qm.used.RealUsed += uint64(fOverhead)
	if qm.used.MaxRealUsed < qm.used.RealUsed {
		qm.used.MaxRealUsed = qm.used.RealUsed
	}
}

// subOverhead subtracts a fragment overhead to the internal
// bookkeeping stats.
func (qm *QMalloc) subOverhead(fOverhead uintptr) {
	qm.used.RealUsed -= uint64(fOverhead)
}

// MUsage returns current memory usage values.
func (qm *QMalloc) MUsage() MUsed {
	return qm.used
}

// Init initialises a qmalloc block.
// The parameters are: memory area, the optimize factor
// (optimise for allocation smaller then 2^optBits) and some configuration
// options flags.
// It returns true on success and false otherwise.
func (qm *QMalloc) Init(mem []byte, optBits int, options Options) bool {
	var l uint64
	*qm = QMalloc{} // zero, in case of re-init
	addr := uintptr(unsafe.Pointer(&mem[0]))
	size := uint64(len(mem))
	start := roundUp(uint64(addr))
	if size < (start - uint64(addr)) {
		return false
	}
	// make sure it's a  multiple of RoundTo
	size -= (start - uint64(addr))
	size = roundDown(size)
	initOverhead := roundUp(uint64(fragSizeof + fragEndSizeof))

	if size < initOverhead {
		return false
	}
	if size < 1024 || optBits < 10 || (1<<optBits) > size {
		return false // TODO: error
	}

	mem = mem[start-uint64(addr) : size]

	optFactor := uint32(optBits)
	optSize := uint32(1) << optFactor

	// alternative use compute optFactor from size:
	//  n = bits.Len32(size)
	//  if size & (size-1) == 0 { optFactor =  1 << (n-1) }
	//  else { optFactor = 1 << (n+1-1) }

	hashSize := optSize/RoundTo + (uint32(unsafe.Sizeof(l))*8 - optFactor) + 1

	// make sure mem[] starts with a multiple of RoundTo
	qm.mem = mem
	qm.optSize = optSize
	qm.optFactor = optFactor
	qm.hashSize = hashSize
	qm.options = options
	qm.size = uint64(size)
	qm.addOverhead(uintptr(initOverhead))

	qm.firstFrag = (*qmFrag)(unsafe.Pointer(&mem[0]))
	end := (*qmFrag)(unsafe.Pointer(&mem[size-1]))
	qm.lastFragEnd = end.prevFragEnd()
	qm.firstFrag.size = size - initOverhead
	qm.lastFragEnd.size = qm.firstFrag.size
	qm.firstFrag.check = StartCheckPattern
	qm.lastFragEnd.check1 = EndCheckPattern1
	qm.lastFragEnd.check2 = EndCheckPattern2

	// init free hash
	qm.freeH = make([]qmFragLst, hashSize)
	for h := 0; h < int(hashSize); h++ {
		qm.freeH[h].head.nxtFree = &qm.freeH[h].head
		qm.freeH[h].tail.prevFree = &qm.freeH[h].head
		qm.freeH[h].head.size = 0
		qm.freeH[h].tail.size = 0
	}

	// linnk initial fragment into the free list
	qm.insertFree(qm.firstFrag)
	return true
}

// roundUp rounds up a size to the next RoundTo multiple.
func roundUp(s uint64) uint64 {
	return (s + (RoundTo - 1)) & RoundToMask
}

// roundDown rounds down a size to the next RoundTo multiple.
func roundDown(s uint64) uint64 {
	return s & RoundToMask
}

// getHash returns the hash index for a fragment of size s.
func (qm *QMalloc) getHash(s uint64) int {
	if s < uint64(qm.optSize) {
		return int(s / RoundTo)
	}
	return int(qm.optSize/RoundTo) + bitLen(s) - int(qm.optFactor) + 1
}

// unHash returns the corresponding size for a hash index
// (reverse for getHash)
func (qm *QMalloc) unHash(h int) uint64 {
	if h < int(qm.optSize/RoundTo) {
		return uint64(h) * RoundTo
	}
	return uint64(1) << (uint32(h) - qm.optSize/RoundTo + qm.optFactor - 1)
}

// Available returns how many bytes are available for allocation (free memory).
func (qm *QMalloc) Available() uint64 {
	return qm.size - qm.used.RealUsed
}

// Owns returns whether or not p was allocated with QMalloc
// (the address is inside the QMalloc "heap").
// Behaviour is undefined if p was Free()d.
func (qm *QMalloc) Owns(p unsafe.Pointer) bool {
	if uintptr(p) >= uintptr(unsafe.Pointer(qm.lastFragEnd)) ||
		uintptr(p) < uintptr(qm.firstFrag.addr()) {
		return false
	}
	return true
}

// bitLen returns the first set bit position for s + 1
// (i such that 2^i > s >= 2^(i-1) )
func bitLen(s uint64) int {
	idx := int(unsafe.Sizeof(uint64(0))*8 - 1)
	for (s & (uint64(1) << (unsafe.Sizeof(uint64(0))*8 - 1))) == 0 {
		s <<= 1
		idx--
	}
	// alternative
	//  idx = bits.Len64(size)
	return idx
}

// insertFree returns a free block to the free hash.
func (qm *QMalloc) insertFree(frag *qmFrag) {
	hash := qm.getHash(frag.size)
	f := qm.freeH[hash].head.nxtFree
	for ; f != &qm.freeH[hash].head; f = f.nxtFree {
		if frag.size <= f.size {
			// found a good place
			// (needed for the "big" hash, for the small one it will
			// always be true (since the fragment have the same size)
			break
		}
	}
	// TODO: fix for qmFragLst with head & tail not aligned
	//       check for f == head  &6 if so fEnd = head.tail

	// insert here
	fEnd := f.end()
	prev := fEnd.prevFree
	prev.nxtFree = frag
	frag.end().prevFree = prev
	frag.nxtFree = f
	fEnd.prevFree = frag
	qm.freeH[hash].no++
}

// detachFree removes a fragment from a free list
func (qm *QMalloc) detachFree(frag *qmFrag) {
	// TODO: fix for qmFragLst with head & tail not aligned
	//      (check for next == head && tail.prevFree = prev)
	prev := frag.end().prevFree
	next := frag.nxtFree
	prev.nxtFree = next
	next.end().prevFree = prev
}

// findFree finds a free fragment of at least size and returns a pointer to
// it and its free fragment hash index.
// If no corresponding fragment is found, it returns (nil, -1)
func (qm *QMalloc) findFree(size uint64) (*qmFrag, int) {
	for hash := qm.getHash(size); hash < int(qm.hashSize); hash++ {
		for f := qm.freeH[hash].head.nxtFree; f != &qm.freeH[hash].head; f = f.nxtFree {
			if f.size >= size {
				return f, hash
			}
		}
		// try in a bigger bucket
	}
	return nil, -1
}

// split a fragment into another  one of at least newSize and add the
// "rest" fragment to the free list.
// newSize must be multiple of RoundTo and less then f.size.
// Note that splitFrag assumes the fragment is already on the free list
// and so if it creates a split, it will not decrease the used size,
// but it will increase realUsed with the new fragment overhead.
// It returns true on success and false if the fragment could not be
// split.
func (qm *QMalloc) splitFrag(f *qmFrag, newSize uint64) bool {
	if f.size <= newSize {
		return false
	}
	rest := f.size - newSize
	if qm.FragAvoidance() {
		if !(rest > (uint64(FragOverhead)+uint64(qm.optSize)) ||
			rest >= (uint64(FragOverhead)+newSize)) {
			// the residue fragment is not big enough
			return false
		}
	} else if !(rest > uint64(FragOverhead)+MinFragSize) {
		return false
	}

	f.size = newSize
	// do the actual split
	end := f.end()
	end.size = newSize
	n := f.next() // new rest fragment
	n.size = rest - uint64(FragOverhead)
	n.end().size = n.size
	qm.addOverhead(FragOverhead)
	if qm.BChecks() {
		end.check1 = EndCheckPattern1
		end.check2 = EndCheckPattern2
		n.check = StartCheckPattern
	}
	// reinsert rest fragment in the free list
	qm.insertFree(n)
	return true
}

// tryJoinFreeFrag will try to create a bigger free fragment,
// looking at the next and previous fragments.
// It returns the new joined fragment (which can be the old passed one
// if join was not possible).
// TODO: should be smarter and join free frags only if the
//  number of frags per the f bucket and the join candidates buckets
// exceed some limit (e.g. max allocated  or max allocated per time)
func (qm *QMalloc) tryJoinFreeFrag(f *qmFrag) *qmFrag {
	orig := f
	size := f.size

	// try joining with the next fragment
	next := f.next()
	if uintptr(next.addr()) < uintptr(unsafe.Pointer(qm.lastFragEnd)) &&
		next.isFree() {
		//next in range and free => could be joined
		if qm.Debug() {
			next.debug(qm)
		}
		qm.detachFree(next)
		size += next.size + uint64(FragOverhead)
		qm.subOverhead(FragOverhead)
		qm.freeH[qm.getHash(next.size)].no--
	}
	// try joining with the previous fragment
	if uintptr(unsafe.Pointer(f)) > uintptr(unsafe.Pointer(qm.firstFrag)) {
		prev := f.prev()
		if qm.Debug() {
			prev.debug(qm)
		}
		if prev.isFree() {
			// join
			qm.detachFree(prev)
			size += prev.size + uint64(FragOverhead)
			qm.subOverhead(FragOverhead)
			qm.freeH[qm.getHash(prev.size)].no--
			f = prev
		}
	}
	if f != orig {
		// mark the original joined fragment as "free", using a bogus
		// invalid value, just still be able to detect double frees on the
		// original f address, after the join
		orig.nxtFree = (*qmFrag)(unsafe.Pointer(uintptr(1)))
	}
	f.size = size
	f.end().size = f.size
	return f
}

// MallocUnsafe is the unsafe (not locking) Malloc version.
// For more details see Malloc.
// On failure (out of memory) it return nil.
func (qm *QMalloc) MallocUnsafe(size uint64) unsafe.Pointer {

	size = roundUp(size) // size must be a multiple of RoundTo
	if size > qm.Available() {
		// not enough free memory
		return nil
	}
	f, hash := qm.findFree(size)
	if f == nil {
		// too fragmented, no suitable fragment found
		return nil
	}
	// we found a good fragment
	// => detach it from the free list
	if qm.Debug() {
		f.debug(qm)
	}
	qm.detachFree(f)
	// mark it as not free
	f.nxtFree = nil
	qm.freeH[hash].no--
	// always try to split, in case the fragment is way too big
	qm.splitFrag(f, size)
	qm.addUsed(f.size)
	if qm.BChecks() {
		f.check = StartCheckPattern
	}
	return f.addr()
}

// FreeUnsafe releases the memory associated with p
// (p must have been previously allocated with MallocUnsafe).
// This is the unsafe non-locking version  (see also Free).
func (qm *QMalloc) FreeUnsafe(p unsafe.Pointer) {
	if p == nil {
		WARN("free(0) called\n")
		return
	}
	if !qm.Owns(p) {
		PANIC("BUG: Free called with pointer %p out of memory block"+
			"(useable range %p-%p, full range %p-%p)\n",
			p, qm.firstFrag.addr(), qm.lastFragEnd,
			&qm.mem[0], &qm.mem[len(qm.mem)-1])
		return
	}
	f := (*qmFrag)(unsafe.Pointer((uintptr(p) - fragSizeof)))
	if qm.Debug() {
		f.debug(qm)
	}
	if f.isFree() {
		PANIC("BUG: attempt to free already freed pointer %p\n", p)
		return
	}
	qm.subUsed(f.size)
	if qm.JoinFree() {
		// expensive operation
		// TODO: join free only if the current bucket contains too
		// many entries and the same goes for the potential joined fragment
		f = qm.tryJoinFreeFrag(f)
	}
	qm.insertFree(f)
}

// ReallocUnsafe tries to grow or shrink a previously malloc allocated
// pointer to a new size.
// This is the unsafe non-locking version. For more details see Realloc.
func (qm *QMalloc) ReallocUnsafe(p unsafe.Pointer, size uint64) unsafe.Pointer {
	if p != nil && !qm.Owns(p) {
		PANIC("BUG: Realloc called with pointer %p out of memory block"+
			"(useable range %p-%p, full range %p-%p)\n",
			p, qm.firstFrag.addr(), qm.lastFragEnd,
			&qm.mem[0], &qm.mem[len(qm.mem)-1])
		return nil
	}
	if size == 0 {
		// it is actually a free
		qm.Free(p)
		return nil
	}
	if p == nil {
		// it's a malloc
		return qm.Malloc(size)
	}
	// actual realloc
	f := (*qmFrag)(unsafe.Pointer((uintptr(p) - fragSizeof)))
	if qm.Debug() {
		f.debug(qm)
	}
	if f.isFree() {
		PANIC("BUG: attempt to realloc an already freed pointer %p\n", p)
		return nil
	}
	// finf first acceptable size
	size = roundUp(size)
	if f.size > size {
		// shrink
		origSize := f.size
		if qm.splitFrag(f, size) {
			// split success => we have the rest fragment added to the
			// free list, but it is not accounted for
			// (splitFrag assumes free fragment and updates only realUsed
			// with the FragOverhead)
			qm.subUsed(origSize - f.size)
		}
	} else if f.size < size {
		// grow
		origSize := f.size
		diff := size - f.size
		n := f.next()
		// TODO: look more then 1 next fragmnet, there could be more in a row
		if uintptr(n.addr()) < uintptr(unsafe.Pointer(qm.lastFragEnd)) &&
			n.isFree() && (n.size+uint64(FragOverhead)) >= diff {
			// join
			qm.detachFree(n)
			qm.freeH[qm.getHash(n.size)].no--
			f.size += n.size + uint64(FragOverhead)
			qm.subOverhead(FragOverhead)
			f.end().size = f.size
			// end checks should be ok (untouched)
			// split if we ended up with a fragment that is too big
			if f.size > size {
				qm.splitFrag(f, size)
			}
			qm.addUsed(f.size - origSize)
		} else {
			// no joining possible => realloc
			ptr := qm.Malloc(size)
			if ptr != nil {
				// copy
				var dst, src []byte
				dstSlice := (*reflect.SliceHeader)(unsafe.Pointer(&dst))
				dstSlice.Data = uintptr(ptr)
				dstSlice.Len = int(size)
				dstSlice.Cap = int(size)
				srcSlice := (*reflect.SliceHeader)(unsafe.Pointer(&src))
				srcSlice.Data = uintptr(p)
				dstSlice.Len = int(origSize)
				dstSlice.Cap = int(origSize)
				copy(dst, src)
				qm.Free(p)
			}
			p = ptr
		}
	} // else roundUp(size) == f.size => do nothing

	return p
}

// Malloc allocates size bytes of memory and returns a pointer to it.
// On failure (out of memory) it return nil.
func (qm *QMalloc) Malloc(size uint64) unsafe.Pointer {
	qm.lock()
	p := qm.MallocUnsafe(size)
	qm.unlock()
	return p
}

// Free releases the memory associated with p (p must have been previously
// allocated with Malloc)
func (qm *QMalloc) Free(p unsafe.Pointer) {
	qm.lock()
	qm.FreeUnsafe(p)
	qm.unlock()
}

// Realloc tries to grow or shrink a previously Malloc allocated pointer to
// a new size.
// It returns either the old value, when the size change was possible in-place,
// or a new value. In the new value case, the old contents is always
// copied in the new location and the old pointer is Free()d.
// If not enough memory is available for growing p, it will return nil,
// but it will _not_ free the original pointer p.
func (qm *QMalloc) Realloc(p unsafe.Pointer, size uint64) unsafe.Pointer {
	qm.lock()
	res := qm.ReallocUnsafe(p, size)
	qm.unlock()
	return res
}
