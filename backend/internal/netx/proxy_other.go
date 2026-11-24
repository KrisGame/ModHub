//go:build !windows

package netx

type winSystemProxy struct {
	Enabled  bool
	Server   string
	Override string
}

func readSystemProxyFromOS() winSystemProxy {
	return winSystemProxy{}
}
