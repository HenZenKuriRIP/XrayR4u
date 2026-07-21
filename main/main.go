package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/HenZenKuriRIP/XrayR4u/main/tools"
	"github.com/HenZenKuriRIP/XrayR4u/panel"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

var (
	configFile   = flag.String("config", "", "Config file for XrayR.")
	printVersion = flag.Bool("version", false, "show version")
)

var (
	version  = "0.9.14"
	codename = "XrayR4u"
	intro    = "Xray backend for K2Board (VLESS + UniProxy)"
)

func showVersion() {
	fmt.Printf("%s %s (%s) \n", codename, version, intro)
}

func getConfig() *viper.Viper {
	config := viper.New()

	// Set custom path and name
	if *configFile != "" {
		configName := path.Base(*configFile)
		configFileExt := path.Ext(*configFile)
		configNameOnly := strings.TrimSuffix(configName, configFileExt)
		configPath := path.Dir(*configFile)
		config.SetConfigName(configNameOnly)
		config.SetConfigType(strings.TrimPrefix(configFileExt, "."))
		config.AddConfigPath(configPath)
		// Set ASSET Path and Config Path for XrayR
		os.Setenv("XRAY_LOCATION_ASSET", configPath)
		os.Setenv("XRAY_LOCATION_CONFIG", configPath)
	} else {
		// Set default config path
		config.SetConfigName("config")
		config.SetConfigType("yml")
		config.AddConfigPath(".")

	}

	if err := config.ReadInConfig(); err != nil {
		log.Panicf("Fatal error config file: %s \n", err)
	}

	config.WatchConfig() // Watch the config

	return config
}

func main() {
	// Tools mode must run before flag.Parse so xray-core command flags work.
	// Examples: XrayR tools x25519 | XrayR vlessenc | XrayR tools help
	if tools.IsToolsInvocation(os.Args) {
		tools.Run(os.Args)
		return
	}

	flag.Parse()
	showVersion()
	if *printVersion {
		return
	}

	config := getConfig()
	panelConfig := &panel.Config{}
	config.Unmarshal(panelConfig)
	p := panel.New(panelConfig)

	// reloadCh serialises hot-reload events onto a single goroutine so that
	// concurrent fsnotify callbacks cannot interleave p.Close()/p.Start().
	// A buffered channel of size 1 acts as a "pending reload" flag: if a
	// second event arrives while a reload is in progress it is coalesced
	// (the channel already has one item) rather than queued infinitely.
	reloadCh := make(chan struct{}, 1)

	// debounce avoids reloading multiple times for a rapid burst of events
	// (e.g. editors that write config files in two steps). Debounce state is
	// owned exclusively by the reload goroutine; OnConfigChange only signals.
	const debounceDuration = 3 * time.Second

	config.OnConfigChange(func(e fsnotify.Event) {
		// Non-blocking send: if the channel is full a reload is already
		// queued, so there is nothing more to do.
		select {
		case reloadCh <- struct{}{}:
		default:
		}
	})

	// Reload loop runs in its own goroutine, sequentially processing signals.
	go func() {
		var lastReload time.Time
		for range reloadCh {
			if time.Since(lastReload) < debounceDuration {
				// A burst slipped through – skip.
				continue
			}
			lastReload = time.Now()
			log.Println("[Panel] Config changed, reloading...")
			p.Close()
			runtime.GC()
			config.Unmarshal(panelConfig)
			p.Start()
			log.Println("[Panel] Reload complete.")
		}
	}()

	p.Start()
	defer p.Close()

	// Explicitly trigger GC to remove garbage from initial config loading.
	runtime.GC()

	// Block until an OS termination signal is received.
	// Note: SIGKILL (os.Kill) cannot be caught; do not register it.
	osSignals := make(chan os.Signal, 1)
	signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
	<-osSignals

	// Close reloadCh so the reload goroutine exits cleanly.
	close(reloadCh)
}
