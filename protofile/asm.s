// +build !gccgo

#include "textflag.h"

// Basically a NOP, to trick the Go compiled into extending lifetime of a variable.
TEXT ·use(SB),NOSPLIT,$0
	RET
