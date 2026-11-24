//go:build windows

package netx

import (
	"syscall"
	"unsafe"
)

const (
	regHKeyCurrentUser = uintptr(0x80000001)
	regKeyQueryValue   = uintptr(0x0001)
	regTypeSZ          = 1
	regTypeExpandSZ    = 2
	regTypeDWORD       = 4
)

var (
	modAdvapi32          = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW    = modAdvapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = modAdvapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = modAdvapi32.NewProc("RegCloseKey")
)

type winSystemProxy struct {
	Enabled  bool
	Server   string
	Override string
}

func regQueryDWORD(hKey uintptr, name string) (uint32, bool) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, false
	}
	var typ uint32
	var val uint32
	size := uint32(unsafe.Sizeof(val))
	ret, _, _ := procRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(namePtr)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&val)),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret != 0 || typ != regTypeDWORD {
		return 0, false
	}
	return val, true
}

func regQueryString(hKey uintptr, name string) (string, bool) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", false
	}
	var typ uint32
	var size uint32
	ret, _, _ := procRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(namePtr)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if ret != 0 || size == 0 || (typ != regTypeSZ && typ != regTypeExpandSZ) {
		return "", false
	}
	buf := make([]uint16, size/2+1)
	ret, _, _ = procRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(namePtr)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret != 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

func readSystemProxyFromOS() winSystemProxy {
	subKey, err := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	if err != nil {
		return winSystemProxy{}
	}
	var hKey uintptr
	ret, _, _ := procRegOpenKeyExW.Call(
		regHKeyCurrentUser,
		uintptr(unsafe.Pointer(subKey)),
		0,
		regKeyQueryValue,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 || hKey == 0 {
		return winSystemProxy{}
	}
	defer procRegCloseKey.Call(hKey)

	en, ok := regQueryDWORD(hKey, "ProxyEnable")
	if !ok || en == 0 {
		return winSystemProxy{}
	}
	srv, _ := regQueryString(hKey, "ProxyServer")
	ovr, _ := regQueryString(hKey, "ProxyOverride")
	return winSystemProxy{
		Enabled:  srv != "",
		Server:   srv,
		Override: ovr,
	}
}
