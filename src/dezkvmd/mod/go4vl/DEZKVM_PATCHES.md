# DezKVM patches to go4vl v0.0.5

This is a vendored copy of https://github.com/vladimirvivien/go4vl (MIT, see LICENSE) with
`examples/`, `docs/` and `multipass/` removed. It is wired in with a `replace` directive in
`dezkvmd/go.mod`.

Why: a USB capture card can glitch or vanish (bad USB link, brown-out, cable wobble). Upstream
panics in that case, which takes the whole daemon down.

## Changes

`v4l2/syscalls.go`
- `send()` keeps the original errno reachable via `errors.Is` (`system error: no such device`),
  so callers can tell a dead device (ENODEV) from a transient EIO.
- New `WaitReadable(fd, timeout)`: a per-call, cancellable `select`. The old `WaitForRead` leaked
  a goroutine forever and busy-looped because it reused a cleared fd set and a spent timeout.

`device/device.go`
- The capture loop never panics. Transient dequeue errors are retried (up to 30 in a row).
  ENODEV/ENXIO/EBADF, or too many errors in a row, end the loop cleanly: the output channel is
  closed, `Done()` fires and `StreamError()` says why.
- The buffer is handed back to the driver *before* the frame is delivered, so a slow consumer
  can not starve the capture card of free buffers.
- Frame delivery is cancellable (the old code could block forever on `d.output <- frame`).
- `Stop()` is idempotent, stops and waits for the loop *before* unmapping buffers (the old code
  could SIGSEGV copying from unmapped memory), and still tries stream-off and unmap if one fails.
- `Close()` always closes the fd, even if `Stop()` fails on an unplugged device.
- New: `Device.Done()` and `Device.StreamError()`.
