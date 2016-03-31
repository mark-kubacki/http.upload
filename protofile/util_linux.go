// +build !appengine

package protofile // import "blitznote.com/src/caddy.upload/protofile"

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// use is a no-op, but the compiler cannot see that it is.
// Calling use(p) ensures that p is kept live until that point.
//go:noescape
func use(p unsafe.Pointer)

// Use this to avoid importing "fmt".
func uitoa(val uint) string {
	var buf [32]byte // big enough for int64
	i := len(buf) - 1
	for val >= 10 {
		buf[i] = byte(val%10 + '0')
		i--
		val /= 10
	}
	buf[i] = byte(val + '0')
	return string(buf[i:])
}

func linkat(olddirfd uintptr, oldpath string, newdirfd int, newpath string, flags int) (err error) {
	var _p0 *byte
	_p0, err = syscall.BytePtrFromString(oldpath)
	if err != nil {
		return
	}
	var _p1 *byte
	_p1, err = syscall.BytePtrFromString(newpath)
	if err != nil {
		return
	}
	_, _, e1 := syscall.Syscall6(unix.SYS_LINKAT, olddirfd, uintptr(unsafe.Pointer(_p0)), uintptr(newdirfd), uintptr(unsafe.Pointer(_p1)), uintptr(flags), 0)
	use(unsafe.Pointer(_p0))
	use(unsafe.Pointer(_p1))
	if e1 != 0 {
		err = e1 //err = errnoErr(e1)
	}
	return
}

func fcntl(fd uintptr, cmd int, arg int) (val int, err error) {
	r0, _, e1 := syscall.Syscall(syscall.SYS_FCNTL, fd, uintptr(cmd), uintptr(arg))
	val = int(r0)
	if e1 != 0 {
		err = e1 // err = errnoErr(e1)
	}
	return
}
