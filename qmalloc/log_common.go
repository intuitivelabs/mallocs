// Copyright 2021 Intuitive Labs GmbH. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE.txt file in the root of the source
// tree.

package qmalloc

// logging functions

import (
	"fmt"

	"github.com/intuitivelabs/slog"
)

// internal constants
const (
	pDBG   = "DBG: " + NAME + ": "
	pWARN  = "WARNING: " + NAME + ": "
	pERR   = "ERROR: " + NAME + ": "
	pBUG   = "BUG: " + NAME + ": "
	pPANIC = NAME + ": "
)

// Log is the generic log
var Log slog.Log = slog.New(slog.LDBG, slog.LbackTraceS|slog.LlocInfoS,
	slog.LStdErr)

// WARNon() is a shorthand for checking if logging at LWARN level is enabled
func WARNon() bool {
	return Log.WARNon()
}

// WARN is a shorthand for logging a warning message.
func WARN(f string, a ...interface{}) {
	Log.LLog(slog.LWARN, 1, pWARN, f, a...)
}

// ERRon() is a shorthand for checking if logging at LERR level is enabled.
func ERRon() bool {
	return Log.ERRon()
}

// ERR is a shorthand for logging an error message.
func ERR(f string, a ...interface{}) {
	Log.LLog(slog.LERR, 1, pERR, f, a...)
}

// BUG is a shorthand for logging a bug message.
func BUG(f string, a ...interface{}) {
	Log.LLog(slog.LBUG, 1, pBUG, f, a...)
}

// PANIC is a shorthand for log + panic.
func PANIC(f string, a ...interface{}) {
	s := fmt.Sprintf(pPANIC+f, a...)
	Log.LLog(slog.LBUG, 1, "", "%s", s)
	panic(s)
}
