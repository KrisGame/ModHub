//go:build windows

package main

import (
	_ "embed"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

//go:embed icon.ico
var trayIconICO []byte

const (
	wmDestroy        = 0x0002
	wmCommand        = 0x0111
	wmUser           = 0x0400
	wmTrayCallback   = wmUser + 1
	wmLButtonUp      = 0x0202
	wmLButtonDblClk  = 0x0203
	wmRButtonUp      = 0x0205
	wmContextMenu    = 0x007B

	nimAdd           = 0x00000000
	nimDelete        = 0x00000002
	nifMessage       = 0x00000001
	nifIcon          = 0x00000002
	nifTip           = 0x00000004

	mfString         = 0x00000000
	mfSeparator      = 0x00000800
	tpmBottomAlign   = 0x0020
	tpmRightButton   = 0x0002

	idiApplication   = 32512

	cmdOpenBrowser   = 1001
	cmdRestartApp    = 1002
	cmdExitApp       = 1003
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetModuleHandleW        = modKernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW        = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW         = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW          = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow           = modUser32.NewProc("DestroyWindow")
	procGetMessageW             = modUser32.NewProc("GetMessageW")
	procTranslateMessage        = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW        = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage         = modUser32.NewProc("PostQuitMessage")
	procPostMessageW            = modUser32.NewProc("PostMessageW")
	procCreatePopupMenu         = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW             = modUser32.NewProc("AppendMenuW")
	procDestroyMenu             = modUser32.NewProc("DestroyMenu")
	procTrackPopupMenu          = modUser32.NewProc("TrackPopupMenu")
	procGetCursorPos            = modUser32.NewProc("GetCursorPos")
	procSetForegroundWindow     = modUser32.NewProc("SetForegroundWindow")
	procLoadIconW               = modUser32.NewProc("LoadIconW")
	procCreateIconFromResEx     = modUser32.NewProc("CreateIconFromResourceEx")
	procLookupIconIdFromDirEx   = modUser32.NewProc("LookupIconIdFromDirectoryEx")
	procDestroyIcon             = modUser32.NewProc("DestroyIcon")
	procShellNotifyIconW        = modShell32.NewProc("Shell_NotifyIconW")
)

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type point struct {
	X int32
	Y int32
}

type msgStruct struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type notifyIconDataW struct {
	CbSize           uint32
	Hwnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type TrayCallbacks struct {
	OnOpen    func()
	OnRestart func()
	OnExit    func()
}

var (
	trayMu       sync.Mutex
	trayCB       TrayCallbacks
	trayHwnd     uintptr
	trayNID      notifyIconDataW
	trayHIcon    uintptr
)

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func loadTrayHIcon() uintptr {
	// Parse ICO directory and load the 32x32 or first PNG/BMP frame via CreateIconFromResourceEx
	if len(trayIconICO) > 22 {
		// ICO header: reserved(2), type(2), count(2), then 16-byte ICONDIRENTRY records
		count := int(trayIconICO[4]) | int(trayIconICO[5])<<8
		if count > 0 && len(trayIconICO) >= 6+count*16 {
			// Pick 32x32 if present, otherwise first entry
			bestIdx := 0
			for i := 0; i < count; i++ {
				off := 6 + i*16
				if trayIconICO[off] == 32 {
					bestIdx = i
					break
				}
			}
			entOff := 6 + bestIdx*16
			imgSize := uint32(trayIconICO[entOff+8]) | uint32(trayIconICO[entOff+9])<<8 | uint32(trayIconICO[entOff+10])<<16 | uint32(trayIconICO[entOff+11])<<24
			imgOffset := uint32(trayIconICO[entOff+12]) | uint32(trayIconICO[entOff+13])<<8 | uint32(trayIconICO[entOff+14])<<16 | uint32(trayIconICO[entOff+15])<<24
			if int(imgOffset+imgSize) <= len(trayIconICO) && imgSize > 0 {
				slice := trayIconICO[imgOffset : imgOffset+imgSize]
				hIcon, _, _ := procCreateIconFromResEx.Call(
					uintptr(unsafe.Pointer(&slice[0])),
					uintptr(imgSize),
					1,          // fIcon = TRUE
					0x00030000, // dwVer
					32,
					32,
					0,
				)
				if hIcon != 0 {
					return hIcon
				}
			}
		}
	}
	hIcon, _, _ := procLoadIconW.Call(0, idiApplication)
	return hIcon
}

func showTrayContextMenu(hwnd uintptr) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	_, _, _ = procAppendMenuW.Call(hMenu, mfString, cmdOpenBrowser, uintptr(unsafe.Pointer(utf16Ptr("Открыть ModHub"))))
	_, _, _ = procAppendMenuW.Call(hMenu, mfString, cmdRestartApp, uintptr(unsafe.Pointer(utf16Ptr("Перезапустить"))))
	_, _, _ = procAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
	_, _, _ = procAppendMenuW.Call(hMenu, mfString, cmdExitApp, uintptr(unsafe.Pointer(utf16Ptr("Выйти"))))

	var pt point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = procSetForegroundWindow.Call(hwnd)
	_, _, _ = procTrackPopupMenu.Call(
		hMenu,
		tpmBottomAlign|tpmRightButton,
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		hwnd,
		0,
	)
	_, _, _ = procPostMessageW.Call(hwnd, 0, 0, 0)
}

func trayWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTrayCallback:
		ev := uint32(lParam & 0xFFFF)
		switch ev {
		case wmLButtonDblClk, wmLButtonUp:
			trayMu.Lock()
			cb := trayCB.OnOpen
			trayMu.Unlock()
			if cb != nil {
				go cb()
			}
		case wmRButtonUp, wmContextMenu:
			showTrayContextMenu(hwnd)
		}
		return 0

	case wmCommand:
		cmd := uint32(wParam & 0xFFFF)
		trayMu.Lock()
		cbs := trayCB
		trayMu.Unlock()
		switch cmd {
		case cmdOpenBrowser:
			if cbs.OnOpen != nil {
				go cbs.OnOpen()
			}
		case cmdRestartApp:
			removeTrayIcon()
			if cbs.OnRestart != nil {
				go cbs.OnRestart()
			}
			_, _, _ = procPostQuitMessage.Call(0)
		case cmdExitApp:
			removeTrayIcon()
			if cbs.OnExit != nil {
				go cbs.OnExit()
			}
			_, _, _ = procPostQuitMessage.Call(0)
		}
		return 0

	case wmDestroy:
		removeTrayIcon()
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func removeTrayIcon() {
	trayMu.Lock()
	nid := trayNID
	hIcon := trayHIcon
	trayNID.Hwnd = 0
	trayHIcon = 0
	trayMu.Unlock()
	if nid.Hwnd != 0 {
		_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	}
	if hIcon != 0 {
		_, _, _ = procDestroyIcon.Call(hIcon)
	}
}

func RunSystemTray(tooltip string, cbs TrayCallbacks) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	trayMu.Lock()
	trayCB = cbs
	trayMu.Unlock()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr("ModHubTrayWindowClass")
	wc := wndClassExW{
		Size:      uint32(unsafe.Sizeof(wndClassExW{})),
		WndProc:   syscall.NewCallback(trayWndProc),
		Instance:  hInst,
		ClassName: className,
	}
	_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16Ptr("ModHubTray"))),
		0,
		0, 0, 0, 0,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		select {}
	}

	hIcon := loadTrayHIcon()
	var nid notifyIconDataW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.Hwnd = hwnd
	nid.UID = 1
	nid.UFlags = nifMessage | nifIcon | nifTip
	nid.UCallbackMessage = wmTrayCallback
	nid.HIcon = hIcon
	tipUTF16, _ := syscall.UTF16FromString(tooltip)
	copy(nid.SzTip[:len(nid.SzTip)-1], tipUTF16)

	trayMu.Lock()
	trayHwnd = hwnd
	trayNID = nid
	trayHIcon = hIcon
	trayMu.Unlock()

	_, _, _ = procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))

	var m msgStruct
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	removeTrayIcon()
	_, _, _ = procDestroyWindow.Call(hwnd)
}
