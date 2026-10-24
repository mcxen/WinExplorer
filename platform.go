package main

// platform.go is the OS contract the app runs on. Every system-tied
// behavior — opening files, terminals, thumbnails, metadata, audio
// fallback, volumes and the trash — is a sys call implemented per OS in
// sys_darwin.go, sys_windows.go, sys_linux.go and sys_other.go, so the
// Explorer behavior above them is written once.
//
// The sys surface:
//
//	sysOpen / sysOpenWith / sysOpenAs   open a file: default app, named
//	                                    app, or the OS's choose-a-app dialog
//	sysReveal                           show a file in the OS file manager
//	sysTerminal / sysTerminalRun        a terminal window, at a folder or
//	                                    running a script
//	sysThumbImage                      a system thumbnail (Quick Look)
//	sysFileMeta                        OS file metadata (mdls)
//	sysAudioPlay                       the OS audio fallback player
//	sysVolumes                         此电脑's volumes
//	sysTrashDir / sysTrashMove         the trash: where it is, and moving
//	sysTrashList / sysTrashKey         an item's listing and restore key
//	sysTrashRestore / sysTrashRemove   putting back, deleting outright
//	sysTrashEmpty                      emptying the whole bin
//	sysTrashRecyclable                 whether delete-to-bin works there (a
//	                                    network share deletes for good, so
//	                                    the caller confirms first)
//	sysKnownFolders                    the sidebar's known folders as the
//	                                    shell resolves them (redirects),
//	                                    nil when home-relative is the truth
//	sysOpenWithScan / sysOpenWithMenu  the 打开方式 registry and its menu
