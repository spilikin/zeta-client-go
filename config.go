// Package zeta provides idiomatic Go bindings for the ZETA Client SDK.
package zeta

import (
	"errors"
	"net/url"
)

// Config is the user-supplied configuration for a Client. Construct as a struct
// literal, then either call Validate or pass to NewClient (which validates).
//
// The shape is implementation-agnostic: no CGO types appear on the surface,
// so a future pure-Go ZETA implementation can satisfy the same Config without
// breaking callers.
type Config struct {
	// ResourceURL is the origin of the resource server (Fachdienst) the client
	// authenticates against. Required; must parse with a scheme.
	ResourceURL string

	// ProductID identifies the client product. Required.
	ProductID string

	// ProductVersion is the client product version. Required.
	ProductVersion string

	// ClientName is a human-readable client identifier. Required.
	ClientName string

	// Auth selects the authentication method. Exactly one of KeystoreAuth,
	// ConnectorAuth, or CustomConnectorAuth. Required.
	Auth Auth

	// Storage is a caller-supplied persistent storage backend. When nil, the
	// SDK's platform-default backend is used and StorageAESKey must be set.
	Storage Storage

	// StorageAESKey is a base64-encoded AES key used to encrypt the platform-
	// default storage at rest. Required when Storage is nil; ignored otherwise.
	StorageAESKey string

	// StoragePath overrides the platform-default storage directory. Linux only;
	// ignored on macOS and Windows.
	StoragePath string

	// Scopes requested at the token endpoint. Required; non-empty.
	Scopes []string

	// ASLProd selects the production ASL endpoint. Default (false) uses test ASL.
	ASLProd bool

	// InsecureSkipTLSVerify disables TLS certificate validation on every HTTPS
	// connection the SDK makes. Use only for test/dev endpoints with self-signed
	// certificates — never against production.
	InsecureSkipTLSVerify bool

	// RequiredRoleOID is the role OID the resource server demands (e.g.
	// "1.2.276.0.76.4.328"). Optional.
	RequiredRoleOID string

	// Proxy configures an outbound HTTP proxy for SDK traffic. Optional.
	Proxy *ProxyConfig

	// Logger receives SDK log lines. Optional; nil leaves the SDK's default
	// stdout logging in place.
	Logger Logger
}

// Auth is a sealed interface satisfied by exactly one of KeystoreAuth,
// ConnectorAuth, or CustomConnectorAuth. The seal (an unexported method)
// prevents external implementations: every variant maps to specific SDK
// behaviour and the binding must understand all of them.
type Auth interface {
	isAuth()
}

// KeystoreAuth authenticates via a PKCS#12 software keystore (SM-B path).
type KeystoreAuth struct {
	File     string // path to the .p12 / .pfx file
	Alias    string // alias of the key entry inside the keystore
	Password string // keystore password
}

func (KeystoreAuth) isAuth() {}

// ConnectorAuth authenticates via the SDK's built-in SMC-B Konnektor SOAP
// client. All six fields are required when this variant is selected.
type ConnectorAuth struct {
	BaseURL        string
	MandantID      string
	ClientSystemID string
	WorkspaceID    string
	UserID         string
	CardHandle     string
}

func (ConnectorAuth) isAuth() {}

// CustomConnectorAuth delegates SMC-B operations to a caller-supplied connector
// implementation. Use this when the built-in SOAP client doesn't match the
// deployment (e.g. a different connector protocol, an HSM, a mock).
type CustomConnectorAuth struct {
	Connector SMCBConnector
}

func (CustomConnectorAuth) isAuth() {}

// SMCBConnector is the contract for a custom SMC-B implementation supplied via
// CustomConnectorAuth.
type SMCBConnector interface {
	// ReadCertificate returns the SMC-B X.509 certificate in DER form.
	ReadCertificate() ([]byte, error)
	// ExternalAuthenticate signs the base64-encoded challenge with the SMC-B
	// card's private key and returns the DER-encoded ECDSA signature.
	ExternalAuthenticate(challenge string) ([]byte, error)
}

// Storage is the contract for a caller-supplied persistent storage backend.
// Implementations MUST be safe for concurrent calls — the SDK invokes these
// from internal coroutine threads.
type Storage interface {
	Put(key, value string) error
	Get(key string) (string, error)
	Remove(key string) error
	Clear() error
}

// ErrNotFound is the sentinel Storage.Get returns for missing keys.
var ErrNotFound = errors.New("zeta: storage key not found")

// Logger receives SDK log output.
type Logger interface {
	Log(level LogLevel, tag, message string)
}

// LogLevel mirrors the SDK's log level enum.
type LogLevel int

// LogLevel values match the SDK's LogLevel ordering (Debug=0 .. Error=3).
const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// ProxyConfig describes an outbound HTTP proxy used by SDK traffic.
type ProxyConfig struct {
	Host     string
	Port     int    // 1..65535
	Username string // optional
	Password string // optional
}

// Validation errors. Returned individually or via errors.Join from Validate.
var (
	ErrMissingResourceURL      = errors.New("zeta: ResourceURL is required")
	ErrInvalidResourceURL      = errors.New("zeta: ResourceURL must parse as an absolute URL with a scheme")
	ErrMissingProductID        = errors.New("zeta: ProductID is required")
	ErrMissingProductVersion   = errors.New("zeta: ProductVersion is required")
	ErrMissingClientName       = errors.New("zeta: ClientName is required")
	ErrMissingAuth             = errors.New("zeta: Auth is required (KeystoreAuth, ConnectorAuth, or CustomConnectorAuth)")
	ErrIncompleteKeystoreAuth  = errors.New("zeta: KeystoreAuth requires File, Alias, and Password")
	ErrIncompleteConnectorAuth = errors.New("zeta: ConnectorAuth requires BaseURL, MandantID, ClientSystemID, WorkspaceID, UserID, and CardHandle")
	ErrMissingCustomConnector  = errors.New("zeta: CustomConnectorAuth.Connector is required")
	ErrMissingStorageAESKey    = errors.New("zeta: StorageAESKey is required when Storage is nil (platform default backend needs an encryption key)")
	ErrEmptyScopes             = errors.New("zeta: Scopes must contain at least one entry")
	ErrInvalidProxyHost        = errors.New("zeta: Proxy.Host is required when Proxy is non-nil")
	ErrInvalidProxyPort        = errors.New("zeta: Proxy.Port must be in 1..65535")
)

// Validate checks the Config against the rules the SDK enforces. Returns nil
// for a valid config; otherwise returns an error joining every failure
// (errors.Is / errors.As work as expected).
//
// NewClient runs Validate internally — call it directly only when you need to
// surface configuration problems before constructing the Client.
func (c Config) Validate() error {
	var errs []error

	if c.ResourceURL == "" {
		errs = append(errs, ErrMissingResourceURL)
	} else if u, err := url.Parse(c.ResourceURL); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, ErrInvalidResourceURL)
	}
	if c.ProductID == "" {
		errs = append(errs, ErrMissingProductID)
	}
	if c.ProductVersion == "" {
		errs = append(errs, ErrMissingProductVersion)
	}
	if c.ClientName == "" {
		errs = append(errs, ErrMissingClientName)
	}

	switch a := c.Auth.(type) {
	case nil:
		errs = append(errs, ErrMissingAuth)
	case KeystoreAuth:
		if a.File == "" || a.Alias == "" || a.Password == "" {
			errs = append(errs, ErrIncompleteKeystoreAuth)
		}
	case ConnectorAuth:
		if a.BaseURL == "" || a.MandantID == "" || a.ClientSystemID == "" ||
			a.WorkspaceID == "" || a.UserID == "" || a.CardHandle == "" {
			errs = append(errs, ErrIncompleteConnectorAuth)
		}
	case CustomConnectorAuth:
		if a.Connector == nil {
			errs = append(errs, ErrMissingCustomConnector)
		}
	}

	if c.Storage == nil && c.StorageAESKey == "" {
		errs = append(errs, ErrMissingStorageAESKey)
	}

	if len(c.Scopes) == 0 {
		errs = append(errs, ErrEmptyScopes)
	}

	if c.Proxy != nil {
		if c.Proxy.Host == "" {
			errs = append(errs, ErrInvalidProxyHost)
		}
		if c.Proxy.Port < 1 || c.Proxy.Port > 65535 {
			errs = append(errs, ErrInvalidProxyPort)
		}
	}

	return errors.Join(errs...)
}
