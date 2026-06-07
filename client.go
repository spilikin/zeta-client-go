package zeta

import (
	"github.com/gematik/zeta-client-go/internal/sdk"
)

// loggerAdapter bridges the public Logger (taking a typed LogLevel) to the
// internal/sdk.Logger interface (taking int). Structural typing won't auto-
// convert between named int types across packages, so we adapt explicitly.
type loggerAdapter struct{ inner Logger }

func (a loggerAdapter) Log(level int, tag, message string) {
	a.inner.Log(LogLevel(level), tag, message)
}

// Client is an authenticated ZETA SDK client. Construct via NewClient. Close
// when finished; the wrapper holds C-side resources that the SDK keeps alive
// for the client's lifetime.
type Client struct {
	inner   *sdk.Client
	storage Storage // user-supplied; nil when using SDK default backend
}

// NewClient validates cfg and constructs a Client backed by the SDK. Returns
// the joined validation error from cfg.Validate, or a SDK-side error if the
// underlying ZetaSdk_buildZetaClient call fails.
func NewClient(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	spec, err := toClientSpec(cfg)
	if err != nil {
		return nil, err
	}
	inner, err := sdk.BuildClient(spec)
	if err != nil {
		return nil, err
	}
	return &Client{inner: inner, storage: cfg.Storage}, nil
}

// Close releases the underlying SDK client and every C allocation behind it.
// Idempotent. Any HTTPClient derived from this Client MUST be closed first.
func (c *Client) Close() {
	c.inner.Close()
}

// HTTPClient derives an HTTPClient bound to this Client. The returned
// HTTPClient must be Close()d before the parent Client.
func (c *Client) HTTPClient() (*HTTPClient, error) {
	inner, err := sdk.NewHTTPClient(c.inner)
	if err != nil {
		return nil, err
	}
	return &HTTPClient{inner: inner}, nil
}

func toClientSpec(cfg Config) (sdk.ClientSpec, error) {
	spec := sdk.ClientSpec{
		ResourceURL:           cfg.ResourceURL,
		ProductID:             cfg.ProductID,
		ProductVersion:        cfg.ProductVersion,
		ClientName:            cfg.ClientName,
		Scopes:                cfg.Scopes,
		Storage:               cfg.Storage,
		AESKey:                cfg.StorageAESKey,
		StoragePath:           cfg.StoragePath,
		ASLProd:               cfg.ASLProd,
		RequiredRoleOID:       cfg.RequiredRoleOID,
		InsecureSkipTLSVerify: cfg.InsecureSkipTLSVerify,
	}
	if cfg.Logger != nil {
		spec.Logger = loggerAdapter{cfg.Logger}
	}
	switch a := cfg.Auth.(type) {
	case KeystoreAuth:
		spec.Keystore = &sdk.KeystoreSpec{File: a.File, Alias: a.Alias, Password: a.Password}
	case ConnectorAuth:
		spec.Connector = &sdk.ConnectorSpec{
			BaseURL: a.BaseURL, MandantID: a.MandantID, ClientSystemID: a.ClientSystemID,
			WorkspaceID: a.WorkspaceID, UserID: a.UserID, CardHandle: a.CardHandle,
		}
	case CustomConnectorAuth:
		spec.CustomConnector = a.Connector
	}
	if cfg.Proxy != nil {
		spec.Proxy = &sdk.ProxySpec{
			Host: cfg.Proxy.Host, Port: cfg.Proxy.Port,
			Username: cfg.Proxy.Username, Password: cfg.Proxy.Password,
		}
	}
	return spec, nil
}
