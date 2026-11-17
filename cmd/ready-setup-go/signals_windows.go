package main

import "os"

// Windows console interrupts can cancel native work. TerminateProcess (including
// Node child.kill) is forceful and cannot run deferred cleanup.
var terminationSignals = []os.Signal{os.Interrupt}
