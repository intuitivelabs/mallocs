// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package qmalloc

type qmFrag struct {
	size    uint64  // fragment size
	nxtFree *qmFrag // next free block pointer, if nil => not free

	// dbg
	check    uint32 // canary used for checking overflows/underflows
	reserved uint32 // not used for now, needed to allign to 16
}

type qmFragEnd struct {
	// dbg
	check1 uint32 // canary, check overflow
	check2 uint32

	// non dbg
	size     uint64
	prevFree *qmFrag
}

type qmFragLst struct {
	// alignment is important, qmFragEnd should be directyl after qmFrag
	// (they should look like a 0-length fragment)
	head qmFrag
	tail qmFragEnd
	no   uint64 // counter
	// TODO: lock per lst?
}

const (
	StartCheckPattern uint32 = 0xf0f0f0f0
	EndCheckPattern1  uint32 = 0xc0c0c0c0
	EndCheckPattern2  uint32 = 0xabcdefed
)

// debug is a helper function that does sanity checks on a fragment.
// On failure it panics (corrupted).
func (f *qmFrag) debug(qm *QMalloc) {
	if f.check != StartCheckPattern {
		qm.dumpStatus()
		PANIC("BUG: qm fragment %p (address %p) "+
			"beginning overwritten (%x)!\n",
			f, f.addr(), f.check)
	}
	fEnd := f.end()
	if fEnd.check1 != EndCheckPattern1 ||
		fEnd.check2 != EndCheckPattern2 {
		qm.dumpStatus()
		PANIC("BUG: qm fragment %p (address %p) "+
			"end overwritten (%x %x)!\n",
			f, f.addr(), fEnd.check1, fEnd.check2)
	}
	if f != qm.firstFrag &&
		(f.prevFragEnd().check1 != EndCheckPattern1 ||
			f.prevFragEnd().check2 != EndCheckPattern2) {
		qm.dumpStatus()
		PANIC("BUG: qm fragment %p (address %p) "+
			"previous fragment end overwritten (%x %x)!\n",
			f, f.addr(), f.prevFragEnd().check1, f.prevFragEnd().check2)
	}
}
