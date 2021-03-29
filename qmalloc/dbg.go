// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package qmalloc

import (
	"unsafe"

	"github.com/intuitivelabs/slog"
)

// dumpStatus will write current status information in the log
func (qm *QMalloc) dumpStatus() {
	const lev = slog.LDBG
	const prefix = "qm_status "

	if !Log.L(lev) {
		return
	}
	Log.LLog(lev, 0, prefix, "(%p):\n", qm)
	if qm == nil {
		return
	}
	Log.LLog(lev, 0, prefix, "heap size= %d\n", qm.size)
	Log.LLog(lev, 0, prefix, "used= %d, used+overhead=%d, free=%d\n",
		qm.used.Used, qm.used.RealUsed, qm.Available())
	Log.LLog(lev, 0, prefix, "max used (+overhead)= %d\n",
		qm.used.MaxRealUsed)
	if qm.options&QMDumpStatsShort != 0 {
		return
	}
	Log.LLog(lev, 0, prefix, "dumping all alloc'ed fragments: %d\n")
	i := 0
	for f := qm.firstFrag; uintptr(unsafe.Pointer(f)) < uintptr(unsafe.Pointer(qm.lastFragEnd)); f = f.next() {
		if !f.isFree() {
			Log.LLog(lev, 0, prefix,
				"   %3d.    address=%p frag=%p size=%d\n",
				i, f.addr(), f, f.size)
			if qm.Debug() {
				Log.LLog(lev, 0, prefix,
					"         start check=%lx, end check= %x, %x\n",
					f.check, f.end().check1, f.end().check2)

			}
		}
		i++
	}
	Log.LLog(lev, 0, prefix, "dumping free list stats: %d\n")
	for h, i := 0, 0; uint32(h) < qm.hashSize; h++ {
		j := uint64(0)
		for f := qm.freeH[h].head.nxtFree; f != &qm.freeH[h].head; f = f.nxtFree {

			i++
			j++
		}
		if j != 0 {
			maxSz := qm.unHash(h)
			if uint32(h) > qm.optSize/RoundTo {
				maxSz *= 2
			}
			Log.LLog(lev, 0, prefix,
				"hash= %3d. fragments no.: %5d\n"+
					"\t\t bucket size: %9lu - %9ld (first %9lu)\n",
				h, j, qm.unHash(h), maxSz, qm.freeH[h].head.nxtFree.size)
		}
		if j != qm.freeH[h].no {
			BUG("qm_status: different free frag count: %d != %d"+
				" for hash %3d\n",
				j, qm.freeH[h].no, h)
		}
	}
	Log.LLog(lev, 0, prefix, "-----------------------------\n")
}
