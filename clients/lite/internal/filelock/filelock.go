// Package filelock takes exclusive, non-blocking locks on open files. The
// OS drops a lock when its file is closed or the process dies, so a crash
// never leaves a stale lock behind.
package filelock
