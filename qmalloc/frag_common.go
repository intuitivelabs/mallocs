// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package qmalloc

import (
	"unsafe"
)

const fragSizeof = unsafe.Sizeof(qmFrag{})
const fragEndSizeof = unsafe.Sizeof(qmFragEnd{})
const FragOverhead = fragSizeof + fragEndSizeof

// qmfrag functions & methods that do not depend on build mode (debug/nodebug)

// isFree() returns true if this is a free fragment.
func (f *qmFrag) isFree() bool { return f.nxtFree != nil }

// addr returns the usable address for a fragment
func (f *qmFrag) addr() unsafe.Pointer {
	return unsafe.Pointer(uintptr(unsafe.Pointer(f)) + fragSizeof)

}

// end returns a pointer to the fragment end.
func (f *qmFrag) end() *qmFragEnd {
	end := uintptr(unsafe.Pointer(f)) + fragSizeof + uintptr(f.size)
	return (*qmFragEnd)((unsafe.Pointer(end)))
}

// prevFragEnd returns the end of the previous fragment.
func (f *qmFrag) prevFragEnd() *qmFragEnd {
	prevFragEndOffs := uintptr(unsafe.Pointer(f)) - fragEndSizeof
	return (*qmFragEnd)(unsafe.Pointer(prevFragEndOffs))
}

// next returns a pointer to the next fragment (after f end)
func (f *qmFrag) next() *qmFrag {
	nxt := uintptr(unsafe.Pointer(f.end())) + fragEndSizeof
	return (*qmFrag)(unsafe.Pointer(nxt))
}

// prev returns a pointer to the previous fragmen
func (f *qmFrag) prev() *qmFrag {
	prevFragEnd := f.prevFragEnd()
	prev := uintptr(unsafe.Pointer(f)) - fragEndSizeof -
		uintptr(prevFragEnd.size) - fragSizeof
	return (*qmFrag)(unsafe.Pointer(prev))
}
