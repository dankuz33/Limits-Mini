package main

import (
	"testing"
	"unsafe"
)

func TestWin64StructureLayout(t *testing.T) {
	checks := map[string]struct{ got, want uintptr }{
		"WNDCLASSEXW": {unsafe.Sizeof(wndClass{}), 80}, "MSG": {unsafe.Sizeof(message{}), 48},
		"PAINTSTRUCT": {unsafe.Sizeof(paintStruct{}), 72}, "BITMAPINFOHEADER": {unsafe.Sizeof(bitmapInfo{}), 40},
		"ICONINFO": {unsafe.Sizeof(iconInfo{}), 32}, "NOTIFYICONDATAW": {unsafe.Sizeof(notifyIconData{}), 976},
		"MONITORINFO": {unsafe.Sizeof(monitorInfo{}), 40}, "OPENFILENAMEW": {unsafe.Sizeof(openFileName{}), 152},
		"JOBOBJECT_EXTENDED_LIMIT_INFORMATION": {unsafe.Sizeof(jobExtendedLimit{}), 144},
	}
	for n, c := range checks {
		if c.got != c.want {
			t.Errorf("%s got %d want %d", n, c.got, c.want)
		}
	}
}
