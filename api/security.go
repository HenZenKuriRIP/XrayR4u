package api

import "strings"

func ResolveSecurity(security string, enableXTLS, enableReality, hasRealitySettings bool) (enableTLS bool, tlsType string, enableVision bool) {
	switch strings.ToLower(security) {
	case "reality":
		return true, "reality", true
	case "tls":
		return true, "tls", false
	case "xtls":
		return true, "tls", true
	}

	if enableReality || hasRealitySettings {
		return true, "reality", true
	}
	if enableXTLS {
		return true, "tls", true
	}
	return false, "", false
}
