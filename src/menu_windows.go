package main

import (
	"fmt"
	"unsafe"
)

func menuItem(menu uintptr, id uintptr, label string, checked bool) {
	flags := uintptr(0)
	if checked {
		flags = 8
	}
	user32.NewProc("AppendMenuW").Call(menu, flags, id, ptr(label))
}
func separator(menu uintptr) { user32.NewProc("AppendMenuW").Call(menu, 0x800, 0, 0) }
func newMenu() uintptr       { return callOne(user32.NewProc("CreatePopupMenu")) }
func submenu(parent, child uintptr, label string) {
	user32.NewProc("AppendMenuW").Call(parent, 0x10, child, ptr(label))
}
func (a *application) showMenu(owner uintptr) {
	if a.menuOpen {
		return
	}
	a.menuOpen = true
	defer func() { a.menuOpen = false }()
	c, _ := a.snapshot()
	m := newMenu()
	defer user32.NewProc("DestroyMenu").Call(m)
	menuItem(m, 1, "Обновить сейчас", false)
	menuItem(m, 2, "Лимиты и состояние подключения", false)
	separator(m)
	menuItem(m, 3, "Показывать виджет на рабочем столе", c.ShowWidget)
	menuItem(m, 4, "Поверх всех окон", c.Topmost)
	menuItem(m, 5, "Недельный лимит Claude", c.Weekly)
	menuItem(m, 12, "Второй / недельный лимит Codex", c.CodexWeekly)
	menuItem(m, 6, "Показывать остаток вместо расхода", c.Remaining)
	sm := newMenu()
	for _, n := range []int{80, 100, 125, 150, 175} {
		menuItem(sm, uintptr(1000+n), fmt.Sprintf("%d%%", n), c.Scale == n)
	}
	submenu(m, sm, "Размер виджета")
	tm := newMenu()
	for _, n := range []int{1, 3, 5, 10} {
		menuItem(tm, uintptr(2000+n), fmt.Sprintf("Каждые %d мин", n), c.Interval == n*60)
	}
	submenu(m, tm, "Частота обновления")
	menuItem(m, 7, "Запускать вместе с Windows", startupEnabled())
	separator(m)
	cm := newMenu()
	menuItem(cm, 26, "Подключить Claude Code", false)
	menuItem(cm, 20, "Как подключить аккаунты", false)
	menuItem(cm, 21, "Claude: выбрать .credentials.json", false)
	menuItem(cm, 22, "Codex: выбрать codex.exe", false)
	menuItem(cm, 23, "Вернуть автоматический поиск", false)
	separator(cm)
	menuItem(cm, 24, "Открыть лимиты Claude в браузере", false)
	menuItem(cm, 25, "Открыть Codex в браузере", false)
	submenu(m, cm, "Подключение")
	menuItem(m, 8, "Вернуть виджет в правый нижний угол", false)
	menuItem(m, 9, "Установить и создать ярлык", false)
	menuItem(m, 10, "О программе", false)
	separator(m)
	menuItem(m, 11, "Выход", false)
	var p point
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&p)))
	user32.NewProc("SetForegroundWindow").Call(owner)
	selected, _, _ := user32.NewProc("TrackPopupMenu").Call(m, 0x100|0x2, uintptr(p.X), uintptr(p.Y), 0, owner, 0)
	pPostMessage.Call(owner, 0, 0, 0)
	if selected >= 1000 && selected < 1200 {
		a.change(func(c *Config) { c.Scale = int(selected) - 1000 })
		a.resizeMain(false)
		pShowWindow.Call(a.popup, 0)
		return
	}
	if selected >= 2000 && selected < 2100 {
		a.change(func(c *Config) { c.Interval = (int(selected) - 2000) * 60 })
		a.trigger()
		return
	}
	switch selected {
	case 1:
		a.trigger()
	case 2:
		a.details(owner)
	case 3:
		a.change(func(c *Config) { c.ShowWidget = !c.ShowWidget })
		c, _ = a.snapshot()
		if c.ShowWidget {
			pShowWindow.Call(a.main, 4)
		} else {
			pShowWindow.Call(a.main, 0)
		}
	case 4:
		a.change(func(c *Config) { c.Topmost = !c.Topmost })
		c, _ = a.snapshot()
		setTopmost(a.main, c.Topmost)
	case 5:
		a.change(func(c *Config) { c.Weekly = !c.Weekly })
	case 12:
		a.change(func(c *Config) { c.CodexWeekly = !c.CodexWeekly })
	case 6:
		a.change(func(c *Config) { c.Remaining = !c.Remaining })
	case 7:
		if err := setStartup(!startupEnabled()); err != nil {
			winError(appName, "Не удалось изменить автозапуск. Проверьте доступ к реестру пользователя.")
		}
	case 8:
		a.change(func(c *Config) { c.ShowWidget = true })
		a.resizeMain(true)
		pShowWindow.Call(a.main, 4)
	case 9:
		beginInstallFromWidget()
	case 10:
		winInfo(owner, appName+" "+appVersion,
			"Компактные лимиты Claude Code и Codex.\r\n\r\nПеретащите виджет мышью. ПКМ открывает меню.\r\nНажмите на иконку в трее: компактное окно.\r\nДвойной клик по виджету: подробности.\r\n\r\nБез телеметрии и автоматической загрузки обновлений.\r\nТокены утилита только читает и сама не перезаписывает.\r\nУстановленный Codex может обновлять свою авторизацию.\r\n\r\nЭто не официальное приложение Anthropic или OpenAI.")
	case 11:
		user32.NewProc("DestroyWindow").Call(a.main)
	case 20:
		winInfo(owner, "Подключение Claude и Codex",
			"CLAUDE\r\nClaude Desktop и Claude Code используют разные локальные входы. Открытый Claude Desktop сам по себе не даёт утилите OAuth Claude Code.\r\n\r\nНажмите «Подключить Claude Code» в этом меню и один раз войдите в аккаунт. Limits Mini только читает созданный Claude Code OAuth и не меняет его.\r\n\r\nCODEX\r\nИспользуется локальный Codex app-server. Если Codex уже вошёл в ChatGPT, отдельный вход обычно не нужен.\r\n\r\nClaude: "+claudeCredentialPath(c)+"\r\nCodex: "+codexHome())
	case 26:
		if err := launchClaudeLogin(); err != nil {
			winInfo(owner, "Claude Code", "Claude Code CLI не найден. Открою официальную страницу установки. После установки снова выберите «Подключить Claude Code».")
			openWeb("https://code.claude.com/docs/en/setup")
		}
	case 21:
		if p := chooseFile(owner, "Выберите .credentials.json Claude Code", true); p != "" {
			a.change(func(c *Config) { c.ClaudeFile = p })
			a.clearProvider(0)
			a.trigger()
		}
	case 22:
		if p := chooseFile(owner, "Выберите доверенный codex.exe из установленного Codex", false); p != "" {
			a.change(func(c *Config) { c.CodexExe = p })
			a.clearProvider(1)
			a.trigger()
		}
	case 23:
		a.change(func(c *Config) { c.ClaudeFile = ""; c.CodexExe = "" })
		a.clearProvider(0)
		a.clearProvider(1)
		a.trigger()
	case 24:
		openWeb("https://claude.ai/settings/usage")
	case 25:
		openWeb("https://chatgpt.com/codex")
	}
}
func (a *application) clearProvider(i int) {
	a.mu.Lock()
	a.usages[i] = Usage{Error: "Подключение..."}
	a.mu.Unlock()
	pPostMessage.Call(a.main, wmAppRefresh, 0, 0)
}
