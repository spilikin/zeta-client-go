package zeta

import (
	"testing"
)

func TestToClientSpec_KeystoreAuth(t *testing.T) {
	cfg := Config{
		ResourceURL:    "https://fd.example.test/",
		ProductID:      "p",
		ProductVersion: "1.0",
		ClientName:     "c",
		Auth: KeystoreAuth{
			File: "/k.p12", Alias: "a", Password: "pw",
		},
		StorageAESKey:   "key",
		Scopes:          []string{"s1", "s2"},
		ASLProd:         true,
		RequiredRoleOID: "1.2.3",
	}
	spec, err := toClientSpec(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Keystore == nil {
		t.Fatal("expected Keystore to be set")
	}
	if spec.Connector != nil {
		t.Fatal("expected Connector to be nil")
	}
	if spec.CustomConnector != nil {
		t.Fatal("expected CustomConnector to be nil")
	}
	if spec.Keystore.File != "/k.p12" || spec.Keystore.Alias != "a" || spec.Keystore.Password != "pw" {
		t.Errorf("keystore fields wrong: %+v", spec.Keystore)
	}
	if spec.ResourceURL != cfg.ResourceURL || spec.ProductID != cfg.ProductID ||
		spec.ProductVersion != cfg.ProductVersion || spec.ClientName != cfg.ClientName {
		t.Errorf("identity fields not copied: %+v", spec)
	}
	if !spec.ASLProd {
		t.Error("ASLProd not propagated")
	}
	if spec.RequiredRoleOID != "1.2.3" {
		t.Errorf("RequiredRoleOID not propagated: %q", spec.RequiredRoleOID)
	}
	if len(spec.Scopes) != 2 || spec.Scopes[0] != "s1" || spec.Scopes[1] != "s2" {
		t.Errorf("scopes wrong: %v", spec.Scopes)
	}
	if spec.AESKey != "key" {
		t.Errorf("AESKey wrong: %q", spec.AESKey)
	}
}

func TestToClientSpec_ConnectorAuth(t *testing.T) {
	cfg := Config{
		ResourceURL: "https://fd.example.test/", ProductID: "p", ProductVersion: "1", ClientName: "c",
		Auth: ConnectorAuth{
			BaseURL: "https://k.test", MandantID: "m", ClientSystemID: "cs",
			WorkspaceID: "w", UserID: "u", CardHandle: "ch",
		},
		StorageAESKey: "key", Scopes: []string{"s"},
	}
	spec, err := toClientSpec(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Connector == nil {
		t.Fatal("expected Connector to be set")
	}
	if spec.Keystore != nil || spec.CustomConnector != nil {
		t.Fatal("unexpected other auth variant set")
	}
	if spec.Connector.BaseURL != "https://k.test" || spec.Connector.MandantID != "m" ||
		spec.Connector.ClientSystemID != "cs" || spec.Connector.WorkspaceID != "w" ||
		spec.Connector.UserID != "u" || spec.Connector.CardHandle != "ch" {
		t.Errorf("connector fields wrong: %+v", spec.Connector)
	}
}

type stubConnector struct{}

func (stubConnector) ReadCertificate() ([]byte, error)            { return nil, nil }
func (stubConnector) ExternalAuthenticate(string) ([]byte, error) { return nil, nil }

func TestToClientSpec_CustomConnectorAuth(t *testing.T) {
	cfg := Config{
		ResourceURL: "https://fd.example.test/", ProductID: "p", ProductVersion: "1", ClientName: "c",
		Auth:          CustomConnectorAuth{Connector: stubConnector{}},
		StorageAESKey: "key", Scopes: []string{"s"},
	}
	spec, err := toClientSpec(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.CustomConnector == nil {
		t.Fatal("expected CustomConnector to be set")
	}
	if spec.Keystore != nil || spec.Connector != nil {
		t.Fatal("unexpected other auth variant set")
	}
}

type (
	capturingLogger struct{ entries []logged }
	logged          struct {
		Level   LogLevel
		Tag     string
		Message string
	}
)

func (c *capturingLogger) Log(level LogLevel, tag, message string) {
	c.entries = append(c.entries, logged{level, tag, message})
}

func TestLoggerAdapter_TranslatesLevel(t *testing.T) {
	captured := &capturingLogger{}
	adapter := loggerAdapter{captured}

	adapter.Log(int(LogLevelDebug), "net", "hi")
	adapter.Log(int(LogLevelError), "io", "boom")

	if len(captured.entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(captured.entries))
	}
	if captured.entries[0].Level != LogLevelDebug || captured.entries[0].Tag != "net" ||
		captured.entries[0].Message != "hi" {
		t.Errorf("entry 0: %+v", captured.entries[0])
	}
	if captured.entries[1].Level != LogLevelError || captured.entries[1].Tag != "io" ||
		captured.entries[1].Message != "boom" {
		t.Errorf("entry 1: %+v", captured.entries[1])
	}
}

func TestToClientSpec_LoggerAdapted(t *testing.T) {
	cfg := Config{
		ResourceURL: "https://fd.example.test/", ProductID: "p", ProductVersion: "1", ClientName: "c",
		Auth:          KeystoreAuth{File: "/k", Alias: "a", Password: "p"},
		StorageAESKey: "key", Scopes: []string{"s"},
		Logger: &capturingLogger{},
	}
	spec, err := toClientSpec(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Logger == nil {
		t.Fatal("expected internal Logger to be set")
	}
}

func TestToClientSpec_Proxy(t *testing.T) {
	cfg := Config{
		ResourceURL: "https://fd.example.test/", ProductID: "p", ProductVersion: "1", ClientName: "c",
		Auth:          KeystoreAuth{File: "/k", Alias: "a", Password: "p"},
		StorageAESKey: "key", Scopes: []string{"s"},
		Proxy: &ProxyConfig{Host: "proxy.test", Port: 8080, Username: "u", Password: "p"},
	}
	spec, err := toClientSpec(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Proxy == nil {
		t.Fatal("expected Proxy to be set")
	}
	if spec.Proxy.Host != "proxy.test" || spec.Proxy.Port != 8080 ||
		spec.Proxy.Username != "u" || spec.Proxy.Password != "p" {
		t.Errorf("proxy fields wrong: %+v", spec.Proxy)
	}
}
