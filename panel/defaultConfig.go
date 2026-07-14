package panel

import "github.com/HenZenKuriRIP/XrayR4u/service/controller"

func getDefaultLogConfig() *LogConfig {
	return &LogConfig{
		Level:      "none",
		AccessPath: "",
		ErrorPath:  "",
	}
}

func getDefaultConnetionConfig() *ConnetionConfig {
	return &ConnetionConfig{
		Handshake:    4,
		ConnIdle:     30,
		UplinkOnly:   2,
		DownlinkOnly: 4,
		BufferSize:   64,
	}
}

func getDefaultControllerConfig() *controller.Config {
	return &controller.Config{
		ListenIP:       "127.0.0.1",
		SendIP:         "127.0.0.1",
		UpdatePeriodic: 60,
		DNSType:        "AsIs",
	}
}
