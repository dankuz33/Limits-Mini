package main

import (
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	gdi32             = syscall.NewLazyDLL("gdi32.dll")
	shell32           = syscall.NewLazyDLL("shell32.dll")
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	advapi32          = syscall.NewLazyDLL("advapi32.dll")
	comdlg32          = syscall.NewLazyDLL("comdlg32.dll")
	comctl32          = syscall.NewLazyDLL("comctl32.dll")
	pDefWindowProc    = user32.NewProc("DefWindowProcW")
	pPostMessage      = user32.NewProc("PostMessageW")
	pShowWindow       = user32.NewProc("ShowWindow")
	pSetWindowPos     = user32.NewProc("SetWindowPos")
	pInvalidateRect   = user32.NewProc("InvalidateRect")
	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	pSelectObject     = gdi32.NewProc("SelectObject")
	pDeleteObject     = gdi32.NewProc("DeleteObject")
	pDrawText         = user32.NewProc("DrawTextW")
	pGetTextExtent    = gdi32.NewProc("GetTextExtentPoint32W")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	pSetBkMode        = gdi32.NewProc("SetBkMode")
	pShellNotify      = shell32.NewProc("Shell_NotifyIconW")
	pDestroyIcon      = user32.NewProc("DestroyIcon")
)

func up(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

var nativeStrings = struct {
	sync.Mutex
	data map[string][]uint16
}{data: make(map[string][]uint16)}

// Keep native UTF-16 buffers rooted: a bare uintptr cannot keep Go memory alive.
// These strings are UI labels, paths and notices, never account credentials.
func ptr(s string) uintptr {
	nativeStrings.Lock()
	defer nativeStrings.Unlock()
	u, ok := nativeStrings.data[s]
	if !ok {
		u, _ = syscall.UTF16FromString(s)
		if len(u) == 0 {
			u = []uint16{0}
		}
		nativeStrings.data[s] = u
	}
	return uintptr(unsafe.Pointer(&u[0]))
}
func neg(n int32) uintptr      { return uintptr(int64(n)) }
func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type size struct{ CX, CY int32 }
type wndClass struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClassExtra, WndExtra               int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSmall                          uintptr
}
type message struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type paintStruct struct {
	DC                 uintptr
	Erase              int32
	Paint              rect
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type bitmapInfo struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}
type iconInfo struct {
	Icon               int32
	XHotspot, YHotspot uint32
	Mask, Color        uintptr
}
type notifyIconData struct {
	Size                uint32
	Hwnd                uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	Guid                [16]byte
	BalloonIcon         uintptr
}
type monitorInfo struct {
	Size          uint32
	Monitor, Work rect
	Flags         uint32
}
type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDir, Title            *uint16
	Flags                        uint32
	FileOffset, FileExtension    uint16
	DefExt                       *uint16
	CustData, Hook               uintptr
	TemplateName                 *uint16
	Reserved                     uintptr
	ReservedFlags, FlagsEx       uint32
}

const (
	wmDestroy      = 0x0002
	wmPaint        = 0x000f
	wmClose        = 0x0010
	wmErase        = 0x0014
	wmActivate     = 0x0006
	wmKeyDown      = 0x0100
	wmTimer        = 0x0113
	wmContextMenu  = 0x007b
	wmLeftDown     = 0x0201
	wmLeftUp       = 0x0202
	wmLeftDouble   = 0x0203
	wmRightUp      = 0x0205
	wmExitSizeMove = 0x0232
	wmDpiChanged   = 0x02e0
	wmAppRefresh   = 0x8001
	wmAppTray      = 0x8002
	wmAppShow      = 0x8003
	wmAppExit      = 0x8004
)

func winError(title, text string) { user32.NewProc("MessageBoxW").Call(0, ptr(text), ptr(title), 0x10) }
func winInfo(owner uintptr, title, text string) {
	// TaskDialog follows the current Windows visual language better than the legacy
	// MessageBox used by the first prototype. Fall back gracefully on older systems.
	if p := comctl32.NewProc("TaskDialog"); p.Find() == nil {
		var button int32
		hr, _, _ := p.Call(owner, 0, ptr(title), ptr(""), ptr(text), 1, 0, uintptr(unsafe.Pointer(&button)))
		if int32(hr) >= 0 {
			return
		}
	}
	user32.NewProc("MessageBoxW").Call(owner, ptr(text), ptr(title), 0x40)
}
func winConfirm(owner uintptr, title, text string) bool {
	r, _, _ := user32.NewProc("MessageBoxW").Call(owner, ptr(text), ptr(title), 0x24)
	return r == 6
}
func fill(dc uintptr, r rect, color uintptr) {
	b, _, _ := pCreateSolidBrush.Call(color)
	user32.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&r)), b)
	pDeleteObject.Call(b)
}
func font(height, weight int32, antialias uint32) uintptr {
	h, _, _ := gdi32.NewProc("CreateFontW").Call(neg(-height), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, uintptr(antialias), 0, ptr("Segoe UI Variable Display"))
	return h
}
func textWidth(dc uintptr, s string) int32 {
	u, _ := syscall.UTF16FromString(s)
	var z size
	if len(u) > 1 {
		pGetTextExtent.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&z)))
	}
	return z.CX
}
func drawText(dc uintptr, s string, r rect, c uintptr, flags uint32) {
	pSetTextColor.Call(dc, c)
	pSetBkMode.Call(dc, 1)
	pDrawText.Call(dc, ptr(s), neg(-1), uintptr(unsafe.Pointer(&r)), uintptr(flags|0x800))
}

func strokeRoundRect(dc uintptr, r rect, radius int32, color uintptr, width int32) {
	pen, _, _ := gdi32.NewProc("CreatePen").Call(0, uintptr(width), color)
	oldPen, _, _ := pSelectObject.Call(dc, pen)
	nullBrush, _, _ := gdi32.NewProc("GetStockObject").Call(5) // NULL_BRUSH
	oldBrush, _, _ := pSelectObject.Call(dc, nullBrush)
	gdi32.NewProc("RoundRect").Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius), uintptr(radius))
	pSelectObject.Call(dc, oldBrush)
	pSelectObject.Call(dc, oldPen)
	pDeleteObject.Call(pen)
}

func windowRect(h uintptr) rect {
	var r rect
	user32.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}
func workArea(h uintptr) rect {
	monitor, _, _ := user32.NewProc("MonitorFromWindow").Call(h, 2)
	m := monitorInfo{}
	m.Size = uint32(unsafe.Sizeof(m))
	r, _, _ := user32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&m)))
	if r != 0 {
		return m.Work
	}
	var rc rect
	user32.NewProc("SystemParametersInfoW").Call(0x30, 0, uintptr(unsafe.Pointer(&rc)), 0)
	return rc
}
func setTopmost(h uintptr, on bool) {
	z := neg(-2)
	if on {
		z = neg(-1)
	}
	pSetWindowPos.Call(h, z, 0, 0, 0, 0, 0x0013)
}
func roundWindow(h uintptr, w, ht, r int32) {
	region, _, _ := gdi32.NewProc("CreateRoundRectRgn").Call(0, 0, uintptr(w+1), uintptr(ht+1), uintptr(r), uintptr(r))
	if region != 0 {
		ok, _, _ := user32.NewProc("SetWindowRgn").Call(h, region, 1)
		if ok == 0 {
			pDeleteObject.Call(region)
		}
	}
}
func openWeb(url string) { shell32.NewProc("ShellExecuteW").Call(0, ptr("open"), ptr(url), 0, 0, 1) }
func chooseFile(owner uintptr, title string, jsonFile bool) string {
	buffer := make([]uint16, 32768)
	filter := "Executable (*.exe)\x00*.exe\x00All files\x00*.*\x00\x00"
	if jsonFile {
		filter = "JSON (*.json)\x00*.json\x00All files\x00*.*\x00\x00"
	}
	// UTF16FromString rejects interior NUL; convert the multi-string explicitly.
	f := make([]uint16, len(filter))
	for i, b := range []byte(filter) {
		f[i] = uint16(b)
	}
	o := openFileName{Owner: owner, Filter: &f[0], FilterIndex: 1, File: &buffer[0], MaxFile: uint32(len(buffer)), Title: up(title), Flags: 0x00001000 | 0x00000800 | 0x00000008 | 0x00080000 | 0x02000000}
	o.Size = uint32(unsafe.Sizeof(o))
	r, _, _ := comdlg32.NewProc("GetOpenFileNameW").Call(uintptr(unsafe.Pointer(&o)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer)
}
