package main

import (
	"sync"

	"fyne.io/systray"
)

type desktopTray struct {
	end       func()
	openItem  *systray.MenuItem
	quitItem  *systray.MenuItem
	once      sync.Once
	ready     chan struct{}
	readyOnce sync.Once
}

func newDesktopTray() *desktopTray {
	return &desktopTray{ready: make(chan struct{})}
}

func (t *desktopTray) markReady() {
	t.readyOnce.Do(func() {
		close(t.ready)
	})
}

func (a *App) startTray() bool {
	if !traySupported() {
		return false
	}
	a.mu.Lock()
	if a.tray != nil {
		a.mu.Unlock()
		return true
	}
	t := newDesktopTray()
	a.tray = t
	a.mu.Unlock()

	t.end = startDesktopTray(func() {
		systray.SetIcon(trayIconBytes)
		systray.SetTitle(desktopAppTitle)
		systray.SetTooltip(desktopAppTitle)
		systray.SetOnTapped(func() { a.showFromTray() })
		// Keep secondary/right-click on systray's native menu path.
		systray.SetOnSecondaryTapped(nil)

		labels := trayMenuLabels(a.trayLocale())
		t.openItem = systray.AddMenuItem(labels.openTitle, labels.openTooltip)
		t.quitItem = systray.AddMenuItem(labels.quitTitle, labels.quitTooltip)

		a.mu.Lock()
		a.trayReady = true
		a.mu.Unlock()
		t.markReady()

		a.goSafe("trayOpenLoop", func() {
			for range t.openItem.ClickedCh {
				a.showFromTray()
			}
		})
		a.goSafe("trayQuitLoop", func() {
			for range t.quitItem.ClickedCh {
				a.quitFromTray()
			}
		})
	}, func() {
		a.mu.Lock()
		if a.tray == t {
			a.trayReady = false
			a.tray = nil
		}
		a.mu.Unlock()
	})
	return true
}

func (a *App) stopTray() {
	a.mu.RLock()
	t := a.tray
	a.mu.RUnlock()
	if t == nil || t.end == nil {
		return
	}
	t.once.Do(t.end)
}

func (a *App) updateTrayLocale(locale string) {
	a.mu.RLock()
	t := a.tray
	a.mu.RUnlock()
	if t == nil || t.openItem == nil || t.quitItem == nil {
		return
	}
	labels := trayMenuLabels(locale)
	t.openItem.SetTitle(labels.openTitle)
	t.openItem.SetTooltip(labels.openTooltip)
	t.quitItem.SetTitle(labels.quitTitle)
	t.quitItem.SetTooltip(labels.quitTooltip)
}

func (a *App) trayLocale() string {
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return ""
	}
	return cfg.DesktopLanguage()
}

func (a *App) showFromTray() {
	a.showMainWindow()
}

func (a *App) quitFromTray() {
	a.quitApp()
}

type trayLabels struct {
	openTitle   string
	openTooltip string
	quitTitle   string
	quitTooltip string
}

func trayMenuLabels(locale string) trayLabels {
	if locale == "zh" {
		return trayLabels{
			openTitle:   "打开",
			openTooltip: "打开 Maddog Dev 窗口",
			quitTitle:   "退出",
			quitTooltip: "退出 Maddog Dev",
		}
	}
	return trayLabels{
		openTitle:   "Open",
		openTooltip: "Open the Maddog Dev window",
		quitTitle:   "Quit",
		quitTooltip: "Quit Maddog Dev",
	}
}
