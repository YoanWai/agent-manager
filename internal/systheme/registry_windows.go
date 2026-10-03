//go:build windows

package systheme

import "golang.org/x/sys/windows/registry"

// windowsScheme reads the apps light/dark switch in Settings > Personalization
// > Colors. The value is absent on builds that predate dark mode, which reads
// as unknown.
func windowsScheme() Scheme {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return SchemeUnknown
	}
	defer key.Close()
	light, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return SchemeUnknown
	}
	switch light {
	case 0:
		return SchemeDark
	case 1:
		return SchemeLight
	default:
		return SchemeUnknown
	}
}
