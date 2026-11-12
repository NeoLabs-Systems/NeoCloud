package tray

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
void installReopenHandler(void);
*/
import "C"

var reopen func()

//export goReopen
func goReopen() {
	if reopen != nil {
		go reopen()
	}
}

// onReopen calls open when NeoCloud.app is opened again while it runs: a
// double click in Finder or Launchpad does not start a second process but
// sends the running one a reopen event.
func onReopen(open func()) {
	reopen = open
	C.installReopenHandler()
}
