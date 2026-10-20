package main

import "sunvale/internal/remote"

// remote_glue.go keeps the short name the assistant HTTP test uses for
// password hashing, so that file need not change while the remote-access
// helpers live in internal/remote.

func remoteHashPassword(pass string) string { return remote.HashPassword(pass) }
