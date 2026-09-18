package main

import (
	"bytes"
	"context"
	_ "embed"
	"image"
	_ "image/png"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Icons are cropped from the reference image supplied for this utility.
//
//go:embed assets/claude.png
var claudePNG []byte

//go:embed assets/codex.png
var codexPNG []byte

type pixels struct {
	w, h int32
	data []byte
}

func decodePixels(b []byte) pixels {
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return pixels{}
	}
	r := im.Bounds()
	p := pixels{w: int32(r.Dx()), h: int32(r.Dy()), data: make([]byte, r.Dx()*r.Dy()*4)}
	// StretchDIBits does not alpha-blend PNG pixels. Composite the anti-aliased
	// transparent edge against the widget surface up-front so brand icons stay crisp.
	const br, bg, bbk = 32, 32, 34
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			rr, gg, bb, aa := im.At(r.Min.X+x, r.Min.Y+y).RGBA()
			a := int(aa >> 8)
			r8 := int(rr>>8) + br*(255-a)/255
			g8 := int(gg>>8) + bg*(255-a)/255
			b8 := int(bb>>8) + bbk*(255-a)/255
			i := (y*r.Dx() + x) * 4
			p.data[i] = byte(b8)
			p.data[i+1] = byte(g8)
			p.data[i+2] = byte(r8)
			p.data[i+3] = 255
		}
	}
	return p
}

type application struct {
	main, popup     uintptr
	icons           [2]uintptr
	images          [2]pixels
	mu              sync.RWMutex
	config          Config
	usages          [2]Usage
	refresh         chan struct{}
	cancel          context.CancelFunc
	menuOpen        bool
	demo            bool
	explorerMessage uint32
	appIcon         uintptr
}

var app *application

const className = "LimitsMiniWidget_010"

func main() {
	runtime.LockOSThread()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--install":
			installApp()
			return
		case "--uninstall":
			uninstallApp()
			return
		}
	}
	demo := len(os.Args) > 1 && os.Args[1] == "--demo"
	mutexName := "Local\\LimitsMini_010"
	if demo {
		mutexName += "_Demo"
	}
	mutex, _, last := kernel32.NewProc("CreateMutexW").Call(0, 1, ptr(mutexName))
	if mutex == 0 {
		winError(appName, "Не удалось создать блокировку приложения.")
		return
	}
	defer syscall.CloseHandle(syscall.Handle(mutex))
	if last == syscall.Errno(183) {
		h, _, _ := user32.NewProc("FindWindowW").Call(ptr(className), 0)
		if h != 0 {
			pPostMessage.Call(h, wmAppShow, 0, 0)
		}
		return
	}
	if p := user32.NewProc("SetProcessDpiAwarenessContext"); p.Find() == nil {
		p.Call(neg(-4))
	} else {
		user32.NewProc("SetProcessDPIAware").Call()
	}
	configureNativeHTTP()
	c := loadConfig()
	if demo {
		c = defaultConfig()
	}
	ctx, cancel := context.WithCancel(context.Background())
	app = &application{config: c, refresh: make(chan struct{}, 1), cancel: cancel, demo: demo}
	defer cancel()
	app.images = [2]pixels{decodePixels(claudePNG), decodePixels(codexPNG)}
	for i := range app.usages {
		app.usages[i] = Usage{Error: "Подключение..."}
	}
	if demo {
		app.usages = [2]Usage{
			{Session: &Limit{Used: 46, Minutes: 300, Reset: time.Now().Add(2 * time.Hour)}, Week: &Limit{Used: 58, Minutes: 10080, Reset: time.Now().Add(3 * 24 * time.Hour)}, At: time.Now(), Source: "DEMO: пример, не реальные данные"},
			{Session: &Limit{Used: 43, Minutes: 300, Reset: time.Now().Add(3 * time.Hour)}, At: time.Now(), Source: "DEMO: пример, не реальные данные"},
		}
	}
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
	app.appIcon = makeTrayIcon(Usage{}, false, 0)
	wc := wndClass{Style: 8 | 0x00020000, WndProc: syscall.NewCallback(windowProc), Instance: instance, Cursor: cursor, Icon: app.appIcon, IconSmall: app.appIcon, ClassName: up(className)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if ok, _, _ := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); ok == 0 {
		winError(appName, "Не удалось зарегистрировать окно.")
		return
	}
	title := appName
	if demo {
		title += " DEMO"
	}
	h, _, err := user32.NewProc("CreateWindowExW").Call(0x80, ptr(className), ptr(title), 0x80000000, uintptr(c.X), uintptr(c.Y), 230, 100, 0, 0, instance, 0)
	if h == 0 {
		winError(appName, "Не удалось создать окно: "+err.Error())
		return
	}
	app.main = h
	h2, _, _ := user32.NewProc("CreateWindowExW").Call(0x80|0x8, ptr(className), ptr(title+" popup"), 0x80000000, 0, 0, 230, 100, 0, 0, instance, 0)
	app.popup = h2
	app.explorerMessage = uint32(callOne(user32.NewProc("RegisterWindowMessageW"), ptr("TaskbarCreated")))
	app.resizeMain(!c.Positioned)
	if c.ShowWidget {
		pShowWindow.Call(app.main, 4)
	}
	app.updateTray(true)
	defer app.removeTray()
	user32.NewProc("SetTimer").Call(app.main, 1, 30000, 0)
	if !demo {
		go app.refreshLoop(ctx)
	}
	var msg message
	for {
		ret, _, _ := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 || int32(ret) == -1 {
			break
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
	}
}
func callOne(p *syscall.LazyProc, args ...uintptr) uintptr { v, _, _ := p.Call(args...); return v }
func (a *application) snapshot() (Config, [2]Usage) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config, a.usages
}
func (a *application) change(f func(*Config)) {
	a.mu.Lock()
	f(&a.config)
	c := a.config
	a.mu.Unlock()
	if !a.demo {
		if err := saveConfig(c); err != nil {
			winError(appName, "Не удалось сохранить настройки.")
		}
	}
	if a.main != 0 {
		a.resizeMain(false)
	}
	pInvalidateRect.Call(a.main, 0, 0)
	if a.popup != 0 {
		pInvalidateRect.Call(a.popup, 0, 0)
	}
	a.updateTray(false)
}
func (a *application) trigger() {
	if a.demo {
		return
	}
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}
func (a *application) refreshLoop(ctx context.Context) {
	var lastAttempt time.Time
	for {
		if delay := 15*time.Second - time.Since(lastAttempt); delay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
		lastAttempt = time.Now()
		c, old := a.snapshot()
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			if old[i].RetryAt.After(time.Now()) {
				continue
			}
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				sub, cancel := context.WithTimeout(ctx, 42*time.Second)
				defer cancel()
				var u Usage
				var err error
				if index == 0 {
					u, err = fetchClaude(sub, c)
				} else {
					u, err = fetchCodex(sub, c)
				}
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					u = Usage{Error: err.Error()}
					msg := strings.ToLower(err.Error())
					auth := strings.Contains(msg, "вход") || strings.Contains(msg, "войдите") || strings.Contains(msg, "токен")
					if old[index].HasData() && !auth {
						u = old[index]
						u.Error = err.Error()
						u.Stale = true
					}
					if fe, ok := err.(*fetchError); ok {
						u.RetryAt = fe.retry
					}
				}
				a.mu.Lock()
				if (index == 0 && a.config.ClaudeFile != c.ClaudeFile) || (index == 1 && a.config.CodexExe != c.CodexExe) {
					a.mu.Unlock()
					return
				}
				a.usages[index] = u
				a.mu.Unlock()
				pPostMessage.Call(a.main, wmAppRefresh, 0, 0)
			}(i)
		}
		wg.Wait()
		c, _ = a.snapshot()
		t := time.NewTimer(time.Duration(c.Interval) * time.Second)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-a.refresh:
			t.Stop()
		case <-t.C:
		}
		// Debounce rapid manual requests. Backoff on HTTP 429 is still enforced above.
		select {
		case <-ctx.Done():
			return
		case <-time.After(600 * time.Millisecond):
		}
	}
}

func scaleFor(h uintptr) float64 {
	c, _ := app.snapshot()
	dpi := uintptr(96)
	if p := user32.NewProc("GetDpiForWindow"); p.Find() == nil {
		if d, _, _ := p.Call(h); d != 0 {
			dpi = d
		}
	}
	return float64(c.Scale) / 100 * float64(dpi) / 96
}
func displayLimits(u Usage, index int, c Config) (*Limit, *Limit) {
	first := u.Session
	if first == nil {
		// Some Codex plans currently expose only the weekly window. Show the useful
		// value instead of an empty primary slot.
		return u.Week, nil
	}
	var second *Limit
	if u.Week != nil && ((index == 0 && c.Weekly) || (index == 1 && c.CodexWeekly)) {
		second = u.Week
	}
	return first, second
}
func rowDisplayText(u Usage, index int, c Config) string {
	first, second := displayLimits(u, index, c)
	if first == nil {
		if u.Error == "Подключение..." {
			return "···"
		}
		return "--"
	}
	v := u.Value(first, c.Remaining)
	if second != nil {
		v += "/" + u.Value(second, c.Remaining)
	}
	return v + "%"
}
func widgetDimensions(hwnd uintptr) (int32, int32) {
	c, us := app.snapshot()
	s := scaleFor(hwnd)
	maxText := int32(0)
	dc, _, _ := user32.NewProc("GetDC").Call(hwnd)
	if dc != 0 {
		f := font(int32(math.Round(36*s)), 400, 5)
		old, _, _ := pSelectObject.Call(dc, f)
		for i, u := range us {
			if w := textWidth(dc, rowDisplayText(u, i, c)); w > maxText {
				maxText = w
			}
		}
		pSelectObject.Call(dc, old)
		pDeleteObject.Call(f)
		user32.NewProc("ReleaseDC").Call(hwnd, dc)
	}
	if maxText == 0 {
		maxText = int32(86 * s)
	}
	w := int32(math.Round(64*s)) + maxText + int32(math.Round(14*s))
	minW, maxW := int32(math.Round(166*s)), int32(math.Round(270*s))
	if w < minW {
		w = minW
	}
	if w > maxW {
		w = maxW
	}
	h := int32(math.Round(100 * s))
	return w, h
}
func (a *application) resizeMain(reset bool) {
	c, _ := a.snapshot()
	s := scaleFor(a.main)
	w, h := widgetDimensions(a.main)
	area := workArea(a.main)
	x, y := c.X, c.Y
	if reset {
		x = area.Right - w - int32(24*s)
		y = area.Bottom - h - int32(24*s)
	}
	if x < area.Left {
		x = area.Left
	}
	if y < area.Top {
		y = area.Top
	}
	if x+w > area.Right {
		x = area.Right - w
	}
	if y+h > area.Bottom {
		y = area.Bottom - h
	}
	z := neg(-2)
	if c.Topmost {
		z = neg(-1)
	}
	pSetWindowPos.Call(a.main, z, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0x0010)
	roundWindow(a.main, w, h, int32(18*s))
	a.mu.Lock()
	a.config.X = x
	a.config.Y = y
	a.config.Positioned = true
	c = a.config
	a.mu.Unlock()
	if !a.demo {
		_ = saveConfig(c)
	}
}
func (a *application) togglePopup() {
	if a.popup == 0 {
		return
	}
	if v, _, _ := user32.NewProc("IsWindowVisible").Call(a.popup); v != 0 {
		pShowWindow.Call(a.popup, 0)
		return
	}
	var p point
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&p)))
	pSetWindowPos.Call(a.popup, neg(-1), uintptr(p.X), uintptr(p.Y), 0, 0, 0x0011)
	s := scaleFor(a.popup)
	w, h := widgetDimensions(a.popup)
	area := workArea(a.popup)
	x, y := p.X-w/2, p.Y-h-14
	if x < area.Left+6 {
		x = area.Left + 6
	}
	if x+w > area.Right-6 {
		x = area.Right - w - 6
	}
	if y < area.Top+6 {
		y = area.Top + 6
	}
	if y+h > area.Bottom-6 {
		y = area.Bottom - h - 6
	}
	pSetWindowPos.Call(a.popup, neg(-1), uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0x0040)
	roundWindow(a.popup, w, h, int32(18*s))
	user32.NewProc("SetForegroundWindow").Call(a.popup)
	pInvalidateRect.Call(a.popup, 0, 0)
}
func (a *application) savePosition() {
	r := windowRect(a.main)
	a.change(func(c *Config) { c.X = r.Left; c.Y = r.Top; c.Positioned = true })
}
func windowProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	if app == nil {
		r, _, _ := pDefWindowProc.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	if msg == app.explorerMessage && msg != 0 && hwnd == app.main {
		app.updateTray(true)
		return 0
	}
	switch msg {
	case wmPaint:
		paintWindow(hwnd)
		return 0
	case wmErase:
		return 1
	case wmClose:
		if hwnd == app.popup {
			pShowWindow.Call(hwnd, 0)
		} else {
			app.change(func(c *Config) { c.ShowWidget = false })
			pShowWindow.Call(hwnd, 0)
		}
		return 0
	case wmDestroy:
		if hwnd == app.main {
			app.cancel()
			user32.NewProc("PostQuitMessage").Call(0)
		}
		return 0
	case wmActivate:
		if hwnd == app.popup && wp&0xffff == 0 && !app.menuOpen {
			pShowWindow.Call(hwnd, 0)
		}
	case wmLeftDown:
		if hwnd == app.main {
			user32.NewProc("ReleaseCapture").Call()
			user32.NewProc("SendMessageW").Call(hwnd, 0x00a1, 2, 0)
		}
		return 0
	case wmLeftDouble:
		app.details(hwnd)
		return 0
	case wmRightUp, wmContextMenu:
		app.showMenu(hwnd)
		return 0
	case wmExitSizeMove:
		if hwnd == app.main {
			app.savePosition()
		}
		return 0
	case wmDpiChanged:
		if hwnd == app.main {
			r := windowRect(hwnd) // Current physical coordinates; avoid dereferencing foreign message memory.
			app.mu.Lock()
			app.config.X = r.Left
			app.config.Y = r.Top
			app.mu.Unlock()
			app.resizeMain(false)
		}
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmKeyDown:
		if wp == 0x74 {
			app.trigger()
		}
		if wp == 27 {
			pShowWindow.Call(hwnd, 0)
			if hwnd == app.main {
				app.change(func(c *Config) { c.ShowWidget = false })
			}
		}
		return 0
	case wmTimer:
		app.updateTray(false)
		pInvalidateRect.Call(app.main, 0, 0)
		if app.popup != 0 {
			pInvalidateRect.Call(app.popup, 0, 0)
		}
		return 0
	case wmAppRefresh:
		app.updateTray(false)
		app.resizeMain(false)
		pInvalidateRect.Call(app.main, 0, 0)
		if app.popup != 0 {
			pInvalidateRect.Call(app.popup, 0, 0)
		}
		return 0
	case wmAppExit:
		app.cancel()
		user32.NewProc("DestroyWindow").Call(app.main)
		return 0
	case wmAppShow:
		app.change(func(c *Config) { c.ShowWidget = true })
		pShowWindow.Call(app.main, 4)
		return 0
	case wmAppTray:
		event := uint32(lp & 0xffff)
		if event == 0x400 || event == 0x401 || event == wmLeftUp {
			app.togglePopup()
		}
		if event == wmContextMenu || event == wmRightUp {
			app.showMenu(app.main)
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func paintWindow(hwnd uintptr) {
	var ps paintStruct
	dest, _, _ := user32.NewProc("BeginPaint").Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer user32.NewProc("EndPaint").Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var client rect
	user32.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&client)))
	if dest == 0 || client.Right <= 0 || client.Bottom <= 0 {
		return
	}
	dc, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(dest)
	bmp, _, _ := gdi32.NewProc("CreateCompatibleBitmap").Call(dest, uintptr(client.Right), uintptr(client.Bottom))
	old, _, _ := pSelectObject.Call(dc, bmp)
	fill(dc, client, rgb(32, 32, 34))
	c, us := app.snapshot()
	s := scaleFor(hwnd)
	for i, u := range us {
		paintRow(dc, client, s, i, u, c)
	}
	// A subtle Windows 11-style surface outline prevents the widget from blending
	// into dark wallpapers while staying visually quieter than a traditional frame.
	strokeRoundRect(dc, rect{0, 0, client.Right - 1, client.Bottom - 1}, int32(18*s), rgb(56, 56, 60), 1)
	if app.demo {
		f := font(int32(9*s), 500, 5)
		of, _, _ := pSelectObject.Call(dc, f)
		drawText(dc, "DEMO", rect{int32(6 * s), client.Bottom - int32(12*s), int32(46 * s), client.Bottom}, rgb(124, 124, 128), 0)
		pSelectObject.Call(dc, of)
		pDeleteObject.Call(f)
	}
	gdi32.NewProc("BitBlt").Call(dest, 0, 0, uintptr(client.Right), uintptr(client.Bottom), dc, 0, 0, 0x00cc0020)
	pSelectObject.Call(dc, old)
	pDeleteObject.Call(bmp)
	gdi32.NewProc("DeleteDC").Call(dc)
}
func percentColor(l *Limit, index int, stale bool) uintptr {
	if l == nil || stale || (!l.Reset.IsZero() && !time.Now().Before(l.Reset)) {
		return rgb(132, 132, 138)
	}
	if l.Used >= 80 {
		return rgb(255, 99, 112)
	}
	if l.Used >= 50 {
		return rgb(244, 183, 35)
	}
	if index == 0 {
		return rgb(73, 222, 196)
	}
	return rgb(244, 244, 246)
}
func paintRow(dc uintptr, client rect, s float64, index int, u Usage, c Config) {
	y := int32(math.Round(float64(8+46*index) * s))
	iconSize := int32(math.Round(38 * s))
	iconX := int32(math.Round(12 * s))
	im := app.images[index]
	if len(im.data) > 0 {
		bi := bitmapInfo{Size: 40, Width: im.w, Height: -im.h, Planes: 1, BitCount: 32}
		gdi32.NewProc("SetStretchBltMode").Call(dc, 4) // HALFTONE
		gdi32.NewProc("StretchDIBits").Call(dc, uintptr(iconX), uintptr(y), uintptr(iconSize), uintptr(iconSize), 0, 0, uintptr(im.w), uintptr(im.h), uintptr(unsafe.Pointer(&im.data[0])), uintptr(unsafe.Pointer(&bi)), 0, 0x00cc0020)
	}
	first, second := displayLimits(u, index, c)
	values := []string{}
	colors := []uintptr{}
	if first == nil {
		placeholder := "--"
		if u.Error == "Подключение..." {
			placeholder = "···"
		}
		values = append(values, placeholder)
		colors = append(colors, rgb(137, 137, 143))
	} else {
		values = append(values, u.Value(first, c.Remaining))
		colors = append(colors, percentColor(first, index, u.Stale))
		if second != nil {
			values = append(values, "/", u.Value(second, c.Remaining))
			colors = append(colors, rgb(229, 229, 232), percentColor(second, index, u.Stale))
		}
		values = append(values, "%")
		colors = append(colors, rgb(245, 245, 247))
	}
	x := int32(math.Round(62 * s))
	available := client.Right - x - int32(math.Round(12*s))
	fh := int32(math.Round(36 * s))
	f := font(fh, 400, 5)
	old, _, _ := pSelectObject.Call(dc, f)
	width := int32(0)
	for _, v := range values {
		width += textWidth(dc, v)
	}
	if width > available && width > 0 {
		pSelectObject.Call(dc, old)
		pDeleteObject.Call(f)
		fh = int32(math.Max(24*s, float64(fh)*float64(available)/float64(width)))
		f = font(fh, 400, 5)
		old, _, _ = pSelectObject.Call(dc, f)
	}
	for n, v := range values {
		w := textWidth(dc, v)
		drawText(dc, v, rect{x, y - int32(2*s), x + w + int32(2*s), y + iconSize + int32(2*s)}, colors[n], 0x20|0x4)
		x += w
	}
	pSelectObject.Call(dc, old)
	pDeleteObject.Call(f)
	if u.Error != "" && u.Error != "Подключение..." {
		// Small amber status dot: details stay out of the minimal surface until requested.
		r := int32(math.Max(5, 6*s))
		dot := rect{iconX + iconSize - r + int32(2*s), y - int32(1*s), iconX + iconSize + int32(2*s), y + r - int32(1*s)}
		b, _, _ := pCreateSolidBrush.Call(rgb(244, 183, 35))
		prev, _, _ := pSelectObject.Call(dc, b)
		pen, _, _ := gdi32.NewProc("GetStockObject").Call(8)
		op, _, _ := pSelectObject.Call(dc, pen)
		gdi32.NewProc("Ellipse").Call(dc, uintptr(dot.Left), uintptr(dot.Top), uintptr(dot.Right), uintptr(dot.Bottom))
		pSelectObject.Call(dc, op)
		pSelectObject.Call(dc, prev)
		pDeleteObject.Call(b)
	}
}

func makeTrayIcon(u Usage, remaining bool, index int) uintptr {
	const n = 32
	bi := bitmapInfo{Size: 40, Width: n, Height: -n, Planes: 1, BitCount: 32}
	var data unsafe.Pointer
	dc, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(0)
	bm, _, _ := gdi32.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&data)), 0, 0)
	if bm == 0 || data == nil {
		gdi32.NewProc("DeleteDC").Call(dc)
		return 0
	}
	old, _, _ := pSelectObject.Call(dc, bm)
	fill(dc, rect{0, 0, n, n}, 0)
	primary := u.Session
	if primary == nil {
		primary = u.Week
	}
	value := u.Value(primary, remaining)
	if value == "--" {
		value = "?"
	}
	height := int32(29)
	if len(value) >= 3 {
		height = 24
	}
	f := font(height, 600, 4)
	of, _, _ := pSelectObject.Call(dc, f)
	color := percentColor(primary, index, u.Stale)
	drawText(dc, value, rect{0, -1, n, 29}, color, 0x1|0x4|0x20)
	pSelectObject.Call(dc, of)
	pDeleteObject.Call(f)
	stripe := rgb(213, 115, 77)
	if index == 1 {
		stripe = rgb(128, 127, 255)
	}
	fill(dc, rect{7, 30, 25, 32}, stripe)
	gdi32.NewProc("GdiFlush").Call()
	raw := unsafe.Slice((*byte)(data), n*n*4)
	for i := 0; i < len(raw); i += 4 {
		// GDI draws premultiplied RGB on black. Restore alpha for a transparent tray background.
		m := raw[i]
		if raw[i+1] > m {
			m = raw[i+1]
		}
		if raw[i+2] > m {
			m = raw[i+2]
		}
		if m != 0 {
			target := color
			if i/4/n >= 30 {
				target = stripe
			}
			maximum := byte(target & 255)
			if v := byte((target >> 8) & 255); v > maximum {
				maximum = v
			}
			if v := byte((target >> 16) & 255); v > maximum {
				maximum = v
			}
			alpha := 255
			if maximum > 0 {
				alpha = int(m) * 255 / int(maximum)
				if alpha > 255 {
					alpha = 255
				}
			}
			raw[i+3] = byte(alpha)
		}
	}
	maskBits := make([]byte, n*n/8)
	mask, _, _ := gdi32.NewProc("CreateBitmap").Call(n, n, 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	ii := iconInfo{Icon: 1, Mask: mask, Color: bm}
	icon, _, _ := user32.NewProc("CreateIconIndirect").Call(uintptr(unsafe.Pointer(&ii)))
	pSelectObject.Call(dc, old)
	pDeleteObject.Call(bm)
	pDeleteObject.Call(mask)
	gdi32.NewProc("DeleteDC").Call(dc)
	return icon
}
func (a *application) updateTray(add bool) {
	if a.main == 0 {
		return
	}
	c, us := a.snapshot()
	for i, u := range us {
		ico := makeTrayIcon(u, c.Remaining, i)
		if ico == 0 {
			continue
		}
		n := notifyIconData{Hwnd: a.main, ID: uint32(i + 1), Flags: 1 | 2 | 4 | 0x80, Callback: wmAppTray, Icon: ico}
		n.Size = uint32(unsafe.Sizeof(n))
		name := "Claude"
		if i == 1 {
			name = "Codex"
		}
		mode := "исп."
		if c.Remaining {
			mode = "ост."
		}
		primary, secondary := displayLimits(u, i, c)
		value := u.Value(primary, c.Remaining)
		tip := name + " · " + mode + " " + value
		if primary != nil {
			tip += "%"
		}
		if secondary != nil {
			tip += " / " + u.Value(secondary, c.Remaining) + "%"
		}
		if u.Error != "" {
			tip += "\n" + u.Error
		} else if primary != nil {
			tip += "\n" + resetText(primary.Reset)
		}
		if u.Stale {
			tip += "\nДанные устарели"
		}
		if a.demo {
			tip = "DEMO · " + tip
		}
		utf, _ := syscall.UTF16FromString(tip)
		if len(utf) > 127 {
			utf = utf[:127]
		}
		copy(n.Tip[:], utf)
		action := uintptr(1)
		if add {
			action = 0
		}
		ok, _, _ := pShellNotify.Call(action, uintptr(unsafe.Pointer(&n)))
		if ok == 0 && !add {
			pShellNotify.Call(0, uintptr(unsafe.Pointer(&n)))
		}
		n.Version = 4
		pShellNotify.Call(4, uintptr(unsafe.Pointer(&n)))
		previous := a.icons[i]
		a.icons[i] = ico
		if previous != 0 {
			pDestroyIcon.Call(previous)
		}
	}
}
func (a *application) removeTray() {
	for i, h := range a.icons {
		n := notifyIconData{Hwnd: a.main, ID: uint32(i + 1)}
		n.Size = uint32(unsafe.Sizeof(n))
		pShellNotify.Call(2, uintptr(unsafe.Pointer(&n)))
		if h != 0 {
			pDestroyIcon.Call(h)
		}
	}
	if a.appIcon != 0 {
		pDestroyIcon.Call(a.appIcon)
	}
}
func (a *application) details(owner uintptr) {
	c, u := a.snapshot()
	content := describeUsage("CLAUDE", u[0], c.Remaining) + "\r\n\r\n" + describeUsage("CODEX", u[1], c.Remaining)
	content += "\r\n\r\nСлева: основное окно. Справа: недельное/второе окно.\r\nОранжевая точка: ошибка или устаревшие данные.\r\nПКМ: настройки и подключение."
	winInfo(owner, appName, content)
}
