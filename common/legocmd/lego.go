// Let's Encrypt client to go!
// CLI application for generating Let's Encrypt certificates using the ACME package.
package legocmd

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/HenZenKuriRIP/XrayR4u/common/legocmd/cmd"
	"github.com/urfave/cli"
)

var version = "dev"
var defaultPath string

// Safe domain / email / provider patterns (no spaces → no argv injection via Split).
var (
	reDomain   = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*\.?$`)
	reEmail    = regexp.MustCompile(`(?i)^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)
	reProvider = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9_-]{0,63}$`)
)

// blockedEnvKeys must never be set from DNSEnv (process-wide pollution / hijack).
var blockedEnvKeys = map[string]struct{}{
	"PATH": {}, "HOME": {}, "USER": {}, "LOGNAME": {}, "SHELL": {},
	"LD_PRELOAD": {}, "LD_LIBRARY_PATH": {}, "DYLD_INSERT_LIBRARIES": {},
	"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {}, "NO_PROXY": {},
	"http_proxy": {}, "https_proxy": {}, "all_proxy": {}, "no_proxy": {},
	"GOPATH": {}, "GOROOT": {}, "TMPDIR": {}, "TEMP": {}, "TMP": {},
}

// allowedDNSEnvPrefix allows known ACME DNS provider credentials (lego).
// Keys must match prefix or be LEGO_* for lego itself.
func allowedDNSEnvKey(key string) bool {
	if key == "" || strings.ContainsAny(key, "=\x00\n\r") {
		return false
	}
	if _, bad := blockedEnvKeys[key]; bad {
		return false
	}
		upper := strings.ToUpper(key)
	if upper == "PATH" || upper == "HOME" || upper == "USER" || upper == "SHELL" ||
		strings.HasPrefix(upper, "LD_") || strings.HasPrefix(upper, "DYLD_") {
		return false
	}
	if strings.HasPrefix(upper, "LEGO_") {
		return true
	}
	// Common lego DNS provider env prefixes (non-exhaustive; reject dangerous only above).
	// Allow alphanumeric + underscore provider-style keys of reasonable length.
	if len(key) > 128 {
		return false
	}
	for _, r := range key {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

type LegoCMD struct {
	cmdClient *cli.App
}

func New() (*LegoCMD, error) {
	app := cli.NewApp()
	app.Name = "lego"
	app.HelpName = "lego"
	app.Usage = "Let's Encrypt client written in Go"
	app.EnableBashCompletion = true

	app.Version = version
	cli.VersionPrinter = func(c *cli.Context) {
		fmt.Printf("lego version %s %s/%s\n", c.App.Version, runtime.GOOS, runtime.GOARCH)
	}

	// Set default path to configPath/cert
	var path string = ""
	configPath := os.Getenv("XRAY_LOCATION_CONFIG")
	if configPath != "" {
		path = configPath
	} else if cwd, err := os.Getwd(); err == nil {
		path = cwd
	} else {
		path = "."
	}

	defaultPath = filepath.Join(path, "cert")

	app.Flags = cmd.CreateFlags(defaultPath)

	app.Before = cmd.Before

	app.Commands = cmd.CreateCommands()

	lego := &LegoCMD{
		cmdClient: app,
	}

	return lego, nil
}

func validateCertInputs(domain, email string, provider string, needProvider bool) error {
	domain = strings.TrimSpace(domain)
	email = strings.TrimSpace(email)
	if domain == "" || !reDomain.MatchString(domain) {
		return fmt.Errorf("invalid cert domain %q", domain)
	}
	if email == "" || !reEmail.MatchString(email) {
		return fmt.Errorf("invalid cert email %q", email)
	}
	if needProvider {
		provider = strings.TrimSpace(provider)
		if provider == "" || !reProvider.MatchString(provider) {
			return fmt.Errorf("invalid dns provider %q", provider)
		}
	}
	return nil
}

// applyDNSEnv sets DNS provider credentials for the duration of the ACME call.
// Returns a cleanup function that restores previous values (or unsets new keys).
func applyDNSEnv(DNSEnv map[string]string) (cleanup func(), err error) {
	if len(DNSEnv) == 0 {
		return func() {}, nil
	}
	type prev struct {
		key   string
		val   string
		exist bool
	}
	var saved []prev
	for key, value := range DNSEnv {
		if !allowedDNSEnvKey(key) {
			return nil, fmt.Errorf("refusing to set unsafe DNSEnv key %q", key)
		}
		old, ok := os.LookupEnv(key)
		saved = append(saved, prev{key: key, val: old, exist: ok})
		if err := os.Setenv(key, value); err != nil {
			// Roll back already-set keys.
			for i := len(saved) - 2; i >= 0; i-- {
				p := saved[i]
				if p.exist {
					_ = os.Setenv(p.key, p.val)
				} else {
					_ = os.Unsetenv(p.key)
				}
			}
			return nil, err
		}
	}
	return func() {
		for i := len(saved) - 1; i >= 0; i-- {
			p := saved[i]
			if p.exist {
				_ = os.Setenv(p.key, p.val)
			} else {
				_ = os.Unsetenv(p.key)
			}
		}
	}, nil
}

// DNSCert cert a domain using DNS API
func (l *LegoCMD) DNSCert(domain, email, provider string, DNSEnv map[string]string) (CertPath string, KeyPath string, err error) {
	defer func() (string, string, error) {
		// Handle any error
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("unknow panic")
			}
			return "", "", err
		}
		return CertPath, KeyPath, nil
	}()
	if err = validateCertInputs(domain, email, provider, true); err != nil {
		return "", "", err
	}
	cleanup, err := applyDNSEnv(DNSEnv)
	if err != nil {
		return "", "", err
	}
	defer cleanup()

	// First check if the certificate exists
	CertPath, KeyPath, err = checkCertfile(domain)
	if err == nil {
		return CertPath, KeyPath, err
	}

	// Build argv as a slice — never fmt.Sprintf + strings.Split (argv injection).
	args := []string{"lego", "-a", "-d", domain, "-m", email, "--dns", provider, "run"}
	err = l.cmdClient.Run(args)
	if err != nil {
		return "", "", err
	}
	CertPath, KeyPath, err = checkCertfile(domain)
	if err != nil {
		return "", "", err
	}
	return CertPath, KeyPath, nil
}

// HTTPCert cert a domain using http methods
func (l *LegoCMD) HTTPCert(domain, email string) (CertPath string, KeyPath string, err error) {
	defer func() (string, string, error) {
		// Handle any error
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("unknow panic")
			}
			return "", "", err
		}
		return CertPath, KeyPath, nil
	}()
	if err = validateCertInputs(domain, email, "", false); err != nil {
		return "", "", err
	}
	// First check if the certificate exists
	CertPath, KeyPath, err = checkCertfile(domain)
	if err == nil {
		return CertPath, KeyPath, err
	}
	args := []string{"lego", "-a", "-d", domain, "-m", email, "--http", "run"}
	err = l.cmdClient.Run(args)

	if err != nil {
		return "", "", err
	}
	CertPath, KeyPath, err = checkCertfile(domain)
	if err != nil {
		return "", "", err
	}
	return CertPath, KeyPath, nil
}

// RenewCert renew a domain cert
func (l *LegoCMD) RenewCert(domain, email, certMode, provider string, DNSEnv map[string]string) (CertPath string, KeyPath string, err error) {
	defer func() (string, string, error) {
		// Handle any error
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("unknow panic")
			}
			return "", "", err
		}
		return CertPath, KeyPath, nil
	}()
	needProvider := certMode == "dns"
	if err = validateCertInputs(domain, email, provider, needProvider); err != nil {
		return "", "", err
	}

	var args []string
	if certMode == "http" {
		args = []string{"lego", "-a", "-d", domain, "-m", email, "--http", "renew", "--days", "30"}
	} else if certMode == "dns" {
		cleanup, e := applyDNSEnv(DNSEnv)
		if e != nil {
			return "", "", e
		}
		defer cleanup()
		args = []string{"lego", "-a", "-d", domain, "-m", email, "--dns", provider, "renew", "--days", "30"}
	} else {
		return "", "", fmt.Errorf("Unsupport cert mode: %s", certMode)
	}
	err = l.cmdClient.Run(args)

	if err != nil {
		return "", "", err
	}
	CertPath, KeyPath, err = checkCertfile(domain)
	if err != nil {
		return "", "", err
	}
	return CertPath, KeyPath, nil
}

func checkCertfile(domain string) (string, string, error) {
	keyPath := path.Join(defaultPath, "certificates", fmt.Sprintf("%s.key", domain))
	certPath := path.Join(defaultPath, "certificates", fmt.Sprintf("%s.crt", domain))
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("Cert key failed: %s", domain)
	}
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("Cert cert failed: %s", domain)
	}
	absKeyPath, _ := filepath.Abs(keyPath)
	absCertPath, _ := filepath.Abs(certPath)
	return absCertPath, absKeyPath, nil
}
