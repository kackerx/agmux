package board

import (
	"bytes"
	"syscall"
	"testing"
	"unsafe"
)

// What a real pty says, which the faked outQueue of TestTTYWriterWaitsForRoom cannot: TIOCOUTQ counts
// what the terminal has not read, and the slave takes 1024 bytes of a write and no more. If the
// request number is wrong (FIONREAD, say) outQueue is 0 for ever, room is always true and the
// upload is quietly back to being cut by tmux's own output.
func TestOutQueueOnARealPty(t *testing.T) {
	ioctl := func(fd int, req uintptr, arg unsafe.Pointer) syscall.Errno {
		_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
		return e
	}
	master, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(master) })
	var name [128]byte
	for _, c := range []struct { // TIOCPTYGRANT, TIOCPTYUNLK, TIOCPTYGNAME
		req uintptr
		arg unsafe.Pointer
	}{{0x20007454, nil}, {0x20007452, nil}, {0x40807453, unsafe.Pointer(&name[0])}} {
		if e := ioctl(master, c.req, c.arg); e != 0 {
			t.Fatal(e)
		}
	}
	w, err := openClientTTY(string(name[:bytes.IndexByte(name[:], 0)]))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	fd := w.(ttyWriter).fd
	if n, err := syscall.Write(fd, make([]byte, 500)); n != 500 || err != nil {
		t.Fatalf("500 bytes: %d, %v", n, err)
	}
	if q, err := outQueue(fd); q != 500 || err != nil {
		t.Errorf("outQueue after 500 unread bytes: %d, %v", q, err)
	}
	if n, _ := syscall.Write(fd, make([]byte, 4000)); n != 524 { // the queue holds 1024
		t.Errorf("4000 more bytes: %d taken, want 524", n)
	}
	if q, _ := outQueue(fd); q != 1024 {
		t.Errorf("outQueue when full: %d", q)
	}
	if (ttyWriter{fd}).room(1) {
		t.Error("a full queue has room")
	}
	buf := make([]byte, 4096) // the terminal reads
	syscall.SetNonblock(master, true)
	for n, _ := syscall.Read(master, buf); n > 0; n, _ = syscall.Read(master, buf) {
	}
	if q, _ := outQueue(fd); q != 0 || !(ttyWriter{fd}).room(1024) {
		t.Errorf("after the terminal read it all: outQueue %d", q)
	}
}
