// Package remote holds the pieces of the remote-access server that do not
// depend on the window or the file manager state: the OpenList-style JSON
// envelope and request binding, JWT signing and password hashing, virtual
// path cleaning and object typing, name validation, and the shared web UI
// tokens. The handlers themselves stay in package main because they are
// methods on the App and read its configuration and shares.
package remote
