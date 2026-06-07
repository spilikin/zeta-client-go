package zeta_test

import (
	"errors"
	"testing"

	"github.com/gematik/zeta-client-go"
)

// validConfig returns a Config that passes every validation rule. Subtests
// modify a copy to exercise individual failures in isolation.
func validConfig() zeta.Config {
	return zeta.Config{
		ResourceURL:    "https://fachdienst.example.test/",
		ProductID:      "ZETA-Test-Client",
		ProductVersion: "1.0.0",
		ClientName:     "go-client",
		Auth: zeta.KeystoreAuth{
			File:     "/path/to/keystore.p12",
			Alias:    "alias",
			Password: "secret",
		},
		StorageAESKey: "base64aeskey==",
		Scopes:        []string{"zero:audience"},
	}
}

func TestValidate_Valid(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*zeta.Config)
		want   error
	}{
		{"resource url empty", func(c *zeta.Config) { c.ResourceURL = "" }, zeta.ErrMissingResourceURL},
		{"resource url malformed", func(c *zeta.Config) { c.ResourceURL = "not a url" }, zeta.ErrInvalidResourceURL},
		{"resource url no scheme", func(c *zeta.Config) { c.ResourceURL = "fachdienst.example.test/" }, zeta.ErrInvalidResourceURL},
		{"product id empty", func(c *zeta.Config) { c.ProductID = "" }, zeta.ErrMissingProductID},
		{"product version empty", func(c *zeta.Config) { c.ProductVersion = "" }, zeta.ErrMissingProductVersion},
		{"client name empty", func(c *zeta.Config) { c.ClientName = "" }, zeta.ErrMissingClientName},
		{"scopes empty", func(c *zeta.Config) { c.Scopes = nil }, zeta.ErrEmptyScopes},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestValidate_Auth(t *testing.T) {
	t.Run("nil auth", func(t *testing.T) {
		cfg := validConfig()
		cfg.Auth = nil
		if err := cfg.Validate(); !errors.Is(err, zeta.ErrMissingAuth) {
			t.Fatalf("expected ErrMissingAuth, got %v", err)
		}
	})
	t.Run("keystore missing fields", func(t *testing.T) {
		for _, missing := range []zeta.KeystoreAuth{
			{Alias: "a", Password: "p"},     // no File
			{File: "/k.p12", Password: "p"}, // no Alias
			{File: "/k.p12", Alias: "a"},    // no Password
			{},                              // all empty
		} {
			cfg := validConfig()
			cfg.Auth = missing
			if err := cfg.Validate(); !errors.Is(err, zeta.ErrIncompleteKeystoreAuth) {
				t.Fatalf("expected ErrIncompleteKeystoreAuth for %+v, got %v", missing, err)
			}
		}
	})
	t.Run("connector complete passes", func(t *testing.T) {
		cfg := validConfig()
		cfg.Auth = zeta.ConnectorAuth{
			BaseURL: "https://konnektor.example.test", MandantID: "m", ClientSystemID: "cs",
			WorkspaceID: "w", UserID: "u", CardHandle: "ch",
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
	t.Run("connector missing any field fails", func(t *testing.T) {
		base := zeta.ConnectorAuth{
			BaseURL: "https://k.example.test", MandantID: "m", ClientSystemID: "cs",
			WorkspaceID: "w", UserID: "u", CardHandle: "ch",
		}
		mutators := []func(*zeta.ConnectorAuth){
			func(c *zeta.ConnectorAuth) { c.BaseURL = "" },
			func(c *zeta.ConnectorAuth) { c.MandantID = "" },
			func(c *zeta.ConnectorAuth) { c.ClientSystemID = "" },
			func(c *zeta.ConnectorAuth) { c.WorkspaceID = "" },
			func(c *zeta.ConnectorAuth) { c.UserID = "" },
			func(c *zeta.ConnectorAuth) { c.CardHandle = "" },
		}
		for i, m := range mutators {
			a := base
			m(&a)
			cfg := validConfig()
			cfg.Auth = a
			if err := cfg.Validate(); !errors.Is(err, zeta.ErrIncompleteConnectorAuth) {
				t.Fatalf("mutator %d: expected ErrIncompleteConnectorAuth, got %v", i, err)
			}
		}
	})
	t.Run("custom connector requires impl", func(t *testing.T) {
		cfg := validConfig()
		cfg.Auth = zeta.CustomConnectorAuth{Connector: nil}
		if err := cfg.Validate(); !errors.Is(err, zeta.ErrMissingCustomConnector) {
			t.Fatalf("expected ErrMissingCustomConnector, got %v", err)
		}
	})
	t.Run("custom connector with impl passes", func(t *testing.T) {
		cfg := validConfig()
		cfg.Auth = zeta.CustomConnectorAuth{Connector: fakeConnector{}}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
}

func TestValidate_Storage(t *testing.T) {
	t.Run("default storage needs AES key", func(t *testing.T) {
		cfg := validConfig()
		cfg.StorageAESKey = ""
		if err := cfg.Validate(); !errors.Is(err, zeta.ErrMissingStorageAESKey) {
			t.Fatalf("expected ErrMissingStorageAESKey, got %v", err)
		}
	})
	t.Run("custom storage skips AES key requirement", func(t *testing.T) {
		cfg := validConfig()
		cfg.Storage = fakeStorage{}
		cfg.StorageAESKey = ""
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
}

func TestValidate_Proxy(t *testing.T) {
	t.Run("nil proxy is fine", func(t *testing.T) {
		cfg := validConfig()
		cfg.Proxy = nil
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
	t.Run("proxy without host fails", func(t *testing.T) {
		cfg := validConfig()
		cfg.Proxy = &zeta.ProxyConfig{Port: 8080}
		if err := cfg.Validate(); !errors.Is(err, zeta.ErrInvalidProxyHost) {
			t.Fatalf("expected ErrInvalidProxyHost, got %v", err)
		}
	})
	t.Run("proxy with bad port fails", func(t *testing.T) {
		for _, port := range []int{0, -1, 65536} {
			cfg := validConfig()
			cfg.Proxy = &zeta.ProxyConfig{Host: "proxy.test", Port: port}
			if err := cfg.Validate(); !errors.Is(err, zeta.ErrInvalidProxyPort) {
				t.Fatalf("port=%d: expected ErrInvalidProxyPort, got %v", port, err)
			}
		}
	})
}

func TestValidate_AggregatesMultipleErrors(t *testing.T) {
	cfg := zeta.Config{} // every required field empty
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []error{
		zeta.ErrMissingResourceURL,
		zeta.ErrMissingProductID,
		zeta.ErrMissingProductVersion,
		zeta.ErrMissingClientName,
		zeta.ErrMissingAuth,
		zeta.ErrMissingStorageAESKey,
		zeta.ErrEmptyScopes,
	} {
		if !errors.Is(err, want) {
			t.Errorf("aggregated error missing %v", want)
		}
	}
}

// Test stubs ------------------------------------------------------------------

type fakeStorage struct{}

func (fakeStorage) Put(string, string) error   { return nil }
func (fakeStorage) Get(string) (string, error) { return "", zeta.ErrNotFound }
func (fakeStorage) Remove(string) error        { return nil }
func (fakeStorage) Clear() error               { return nil }

type fakeConnector struct{}

func (fakeConnector) ReadCertificate() ([]byte, error)            { return nil, nil }
func (fakeConnector) ExternalAuthenticate(string) ([]byte, error) { return nil, nil }
