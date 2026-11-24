//go:build windows

package runtime

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// discoverRegistryCandidates inspects official Windows Registry keys where JDK and JRE vendors register their installations.
func discoverRegistryCandidates(add func(raw, source string)) {
	prefixes := []string{"SOFTWARE", `SOFTWARE\WOW6432Node`}
	roots := []string{
		`JavaSoft\Java Development Kit`,
		`JavaSoft\Java Runtime Environment`,
		`JavaSoft\JDK`,
		`JavaSoft\JRE`,
		`Eclipse Adoptium\JDK`,
		`Eclipse Adoptium\JRE`,
		`Eclipse Foundation\JDK`,
		`Eclipse Foundation\JRE`,
		`AdoptOpenJDK\JDK`,
		`AdoptOpenJDK\JRE`,
		`Microsoft\JDK`,
		`Microsoft\JRE`,
		`BellSoft\Liberica JDK`,
		`BellSoft\Liberica JRE`,
		`Zulu`,
	}

	for _, pfx := range prefixes {
		for _, root := range roots {
			fullKey := pfx + `\` + root
			k, err := registry.OpenKey(registry.LOCAL_MACHINE, fullKey, registry.READ)
			if err != nil {
				continue
			}
			subkeys, err := k.ReadSubKeyNames(-1)
			k.Close()
			if err != nil {
				continue
			}
			for _, sub := range subkeys {
				subPath := fullKey + `\` + sub
				sk, err := registry.OpenKey(registry.LOCAL_MACHINE, subPath, registry.READ)
				if err != nil {
					continue
				}
				for _, valName := range []string{"JavaHome", "Path", "InstallationPath"} {
					val, _, err := sk.GetStringValue(valName)
					if err == nil && strings.TrimSpace(val) != "" {
						add(val, "registry")
					}
				}

				// Check nested keys (e.g. MS / Adoptium sometimes nest `MSI` or `hotspot` under version)
				nestedSubkeys, _ := sk.ReadSubKeyNames(-1)
				sk.Close()
				for _, nsub := range nestedSubkeys {
					nsk, err := registry.OpenKey(registry.LOCAL_MACHINE, subPath+`\`+nsub, registry.READ)
					if err != nil {
						continue
					}
					for _, valName := range []string{"JavaHome", "Path", "InstallationPath"} {
						val, _, err := nsk.GetStringValue(valName)
						if err == nil && strings.TrimSpace(val) != "" {
							add(val, "registry")
						}
					}
					nsk.Close()
				}
			}
		}
	}
}
