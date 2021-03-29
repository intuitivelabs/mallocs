# qmalloc

[![Go Reference](https://pkg.go.dev/badge/github.com/intuitivelabs/mallocs/qmalloc.svg)](https://pkg.go.dev/github.com/intuitivelabs/mallocs/qmalloc)

qmalloc is a simple malloc implementation with extra debugging information.
It is a port of ser/kamailio qmalloc.
It is optimised for workloads that stabilise to certain block sizes (it does
 not handle high fragmentation).

## Limitations

- does not handle fragmentation well
- uses a single "global" lock so not recommended if allocations are
 done from different goroutines
- needs a "warm-up" time (initial allocations are slower)
