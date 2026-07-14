package anytls

// User mirrors sing-anytls User for use outside the vendor boundary.
type User struct {
	Name     string
	Password string
}

// ServerConfig is the runtime configuration used to construct a Server.
type ServerConfig struct {
	PaddingScheme []byte
	Users         []User
}
