# godbus/dbus v5.2.2, patched for wayle

A copy of github.com/godbus/dbus/v5 v5.2.2 (also unfixed on master
6fea989, 2026-09-24), wired in through go.mod's `replace`. Drop it once
upstream decodes fd arrays.

- `decoder.go`, case `'a'`: an `ah` decoded with the message's fds made a
  `[]UnixFDIndex` and appended `UnixFD` values to it. The reflect panic was
  a string, which `Decode`'s recover does not turn into an error, so every
  message carrying an fd array (xdg-desktop-portal's Email
  `attachment_fds`) arrived with an empty body. The slice is now
  `[]UnixFD`, and an index past the fds is an InvalidMessageError.
  Pinned by `wayle_fd_array_test.go`.
- `encoder.go`, case `variantType`, and `wayle_encode_as.go`: a Variant's
  value is encoded by the Variant's signature. `decode` returns a struct as
  `[]any`, which the Go-type encoder wrote as an array of variants under the
  struct signature: a malformed message, which dbus-daemon answers by
  disconnecting the sender. Echoing a decoded GIcon (DynamicLauncher's
  PrepareInstall) killed the portal's bus connection. Pinned by
  `TestDecodedStructsReencode`.
