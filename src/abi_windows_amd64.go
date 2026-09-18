package main

import "unsafe"

// These compile-time assertions validate the x64 ABI without requiring a Windows runtime.
// Every pair fails compilation if the Go layout differs from the Windows SDK structure.
var (
	_ [unsafe.Sizeof(wndClass{}) - 80]byte
	_ [80 - unsafe.Sizeof(wndClass{})]byte
	_ [unsafe.Sizeof(message{}) - 48]byte
	_ [48 - unsafe.Sizeof(message{})]byte
	_ [unsafe.Sizeof(paintStruct{}) - 72]byte
	_ [72 - unsafe.Sizeof(paintStruct{})]byte
	_ [unsafe.Sizeof(bitmapInfo{}) - 40]byte
	_ [40 - unsafe.Sizeof(bitmapInfo{})]byte
	_ [unsafe.Sizeof(iconInfo{}) - 32]byte
	_ [32 - unsafe.Sizeof(iconInfo{})]byte
	_ [unsafe.Sizeof(notifyIconData{}) - 976]byte
	_ [976 - unsafe.Sizeof(notifyIconData{})]byte
	_ [unsafe.Sizeof(monitorInfo{}) - 40]byte
	_ [40 - unsafe.Sizeof(monitorInfo{})]byte
	_ [unsafe.Sizeof(openFileName{}) - 152]byte
	_ [152 - unsafe.Sizeof(openFileName{})]byte
	_ [unsafe.Sizeof(jobExtendedLimit{}) - 144]byte
	_ [144 - unsafe.Sizeof(jobExtendedLimit{})]byte
)
