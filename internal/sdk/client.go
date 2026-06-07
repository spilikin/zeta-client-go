package sdk

/*
#include <stdlib.h>
#include "zeta_sdk_glue.h"
*/
import "C"

import (
	"errors"
	"log"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
)

// ClientSpec is the internal-side validated input to BuildClient. The public
// zeta.Config translates into this. internal/sdk cannot import zeta (cycle),
// so the shape is mirrored here for the fields the C layer needs.
type ClientSpec struct {
	ResourceURL    string
	ProductID      string
	ProductVersion string
	ClientName     string

	Scopes []string

	// Auth: exactly one of Keystore / Connector / CustomConnector is non-nil.
	// Mutual exclusivity is enforced by the public sealed Auth interface and
	// caught here as a sanity check.
	Keystore        *KeystoreSpec
	Connector       *ConnectorSpec
	CustomConnector SMCBConnector

	// Storage: nil means use the SDK's platform-default backend, in which case
	// AESKey must be set.
	Storage     Storage
	AESKey      string
	StoragePath string // Linux only

	ASLProd               bool
	RequiredRoleOID       string
	InsecureSkipTLSVerify bool

	Proxy  *ProxySpec // optional; nil disables proxy
	Logger Logger     // nil → SDK default stdout logging
}

type KeystoreSpec struct {
	File, Alias, Password string
}

type ConnectorSpec struct {
	BaseURL, MandantID, ClientSystemID, WorkspaceID, UserID, CardHandle string
}

type ProxySpec struct {
	Host     string
	Port     int
	Username string
	Password string
}

// SMCBConnector mirrors the public zeta.SMCBConnector interface so the
// internal layer can hold a value satisfying both via structural typing.
type SMCBConnector interface {
	ReadCertificate() ([]byte, error)
	ExternalAuthenticate(challenge string) ([]byte, error)
}

// Client wraps the SDK's ZetaSdk_Client. The SDK marks ZetaSdk_Client as NOT
// thread-safe — external synchronization required. The mutex serializes every
// SDK call routed through this client (build, HTTP requests, clear).
//
// Lifecycle: explicit Close is the contract; runtime.AddCleanup is the safety
// net. The bundle struct holds every retained allocation; cleanup args
// reference the bundle pointer, not the Client wrapper.
type Client struct {
	mu      sync.Mutex
	bundle  *clientBundle
	closed  *atomic.Bool
	cleanup runtime.Cleanup
}

type clientBundle struct {
	client  unsafe.Pointer // *C.ZetaSdk_Client; nil after free
	storage *StorageHandle // own a custom storage handle if one was provided
	logger  *LoggerHandle  // own a custom logger handle if one was provided
	smcb    *SmcbHandle    // own a custom SMC-B connector handle if one was provided
	frees   []func()       // every CString / malloc made while building configs
}

// BuildClient allocates all C resources, calls ZetaSdk_buildZetaClient, and
// returns a Client that owns them. On any failure before the C call, every
// allocation is rolled back. On a successful SDK call the resources live for
// the lifetime of the Client and are freed by Close.
func BuildClient(spec ClientSpec) (*Client, error) {
	authCount := 0
	if spec.Keystore != nil {
		authCount++
	}
	if spec.Connector != nil {
		authCount++
	}
	if spec.CustomConnector != nil {
		authCount++
	}
	if authCount == 0 {
		return nil, errors.New("sdk: ClientSpec requires Keystore, Connector, or CustomConnector")
	}
	if authCount > 1 {
		return nil, errors.New("sdk: ClientSpec must set exactly one of Keystore, Connector, CustomConnector")
	}

	alloc := &cAllocator{}
	var storageHandle *StorageHandle
	var loggerHandle *LoggerHandle
	var smcbHandle *SmcbHandle
	rollback := func() {
		alloc.Free()
		if storageHandle != nil {
			storageHandle.Close()
		}
		if loggerHandle != nil {
			loggerHandle.Close()
		}
		if smcbHandle != nil {
			smcbHandle.Close()
		}
	}

	scopesArr := (**C.char)(alloc.malloc(C.size_t(len(spec.Scopes)) * C.size_t(unsafe.Sizeof(uintptr(0)))))
	scopesSlice := unsafe.Slice(scopesArr, len(spec.Scopes))
	for i, s := range spec.Scopes {
		scopesSlice[i] = alloc.string(s)
	}

	storageConfig := (*C.ZetaSdk_StorageConfig)(alloc.calloc(1, C.sizeof_ZetaSdk_StorageConfig))
	if spec.AESKey != "" {
		storageConfig.aesB64Key = alloc.string(spec.AESKey)
	}
	if spec.StoragePath != "" {
		storageConfig.storagePath = alloc.string(spec.StoragePath)
	}
	if spec.Storage != nil {
		storageHandle = NewStorageHandle(spec.Storage)
		storageConfig.customStorage = (*C.ZetaSdk_StorageVTable)(storageHandle.VTable())
	}

	tpmConfig := (*C.ZetaSdk_TpmConfig)(alloc.calloc(1, C.sizeof_ZetaSdk_TpmConfig))

	authConfig := (*C.ZetaSdk_AuthConfig)(alloc.calloc(1, C.sizeof_ZetaSdk_AuthConfig))
	authConfig.scopes = scopesArr
	authConfig.scopesCount = C.int(len(spec.Scopes))
	authConfig.exp = 30 // token expiration in seconds; matches the C++ sample
	authConfig.aslProdEnvironment = C.bool(spec.ASLProd)
	if spec.RequiredRoleOID != "" {
		authConfig.requiredOid = alloc.string(spec.RequiredRoleOID)
	}

	switch {
	case spec.Keystore != nil:
		smb := (*C.ZetaSdk_SmbConfig)(alloc.calloc(1, C.sizeof_ZetaSdk_SmbConfig))
		smb.keystoreFile = alloc.string(spec.Keystore.File)
		smb.alias = alloc.string(spec.Keystore.Alias)
		smb.password = alloc.string(spec.Keystore.Password)
		authConfig.smbConfig = smb
	case spec.Connector != nil:
		smcb := (*C.ZetaSdk_SmcbConfig)(alloc.calloc(1, C.sizeof_ZetaSdk_SmcbConfig))
		smcb.baseUrl = alloc.string(spec.Connector.BaseURL)
		smcb.mandantId = alloc.string(spec.Connector.MandantID)
		smcb.clientSystemId = alloc.string(spec.Connector.ClientSystemID)
		smcb.workspaceId = alloc.string(spec.Connector.WorkspaceID)
		smcb.userId = alloc.string(spec.Connector.UserID)
		smcb.cardHandle = alloc.string(spec.Connector.CardHandle)
		authConfig.smcbConfig = smcb
	case spec.CustomConnector != nil:
		smcb := (*C.ZetaSdk_SmcbConfig)(alloc.calloc(1, C.sizeof_ZetaSdk_SmcbConfig))
		smcbHandle = NewSmcbHandle(spec.CustomConnector)
		smcb.customSmcb = (*C.ZetaSdk_SmcbVTable)(smcbHandle.VTable())
		authConfig.smcbConfig = smcb
	}

	buildConfig := (*C.ZetaSdk_BuildConfig)(alloc.calloc(1, C.sizeof_ZetaSdk_BuildConfig))
	buildConfig.resource = alloc.string(spec.ResourceURL)
	buildConfig.productId = alloc.string(spec.ProductID)
	buildConfig.productVersion = alloc.string(spec.ProductVersion)
	buildConfig.clientName = alloc.string(spec.ClientName)
	buildConfig.storageConfig = storageConfig
	buildConfig.tpmConfig = tpmConfig
	buildConfig.authConfig = authConfig
	if spec.Logger != nil {
		loggerHandle = NewLoggerHandle(spec.Logger)
		buildConfig.logVTable = (*C.ZetaSdk_LogVTable)(loggerHandle.VTable())
	}
	if spec.Proxy != nil {
		proxy := (*C.ZetaSdk_ProxyConfig)(alloc.calloc(1, C.sizeof_ZetaSdk_ProxyConfig))
		proxy.host = alloc.string(spec.Proxy.Host)
		proxy.port = C.int(spec.Proxy.Port)
		if spec.Proxy.Username != "" {
			proxy.username = alloc.string(spec.Proxy.Username)
		}
		if spec.Proxy.Password != "" {
			proxy.password = alloc.string(spec.Proxy.Password)
		}
		buildConfig.proxyConfig = proxy
	}

	sdkClient := C.ZetaSdk_buildZetaClient(unsafe.Pointer(buildConfig), C.bool(spec.InsecureSkipTLSVerify))
	if sdkClient == nil {
		rollback()
		return nil, errors.New("sdk: ZetaSdk_buildZetaClient returned NULL")
	}

	bundle := &clientBundle{
		client:  sdkClient,
		storage: storageHandle,
		logger:  loggerHandle,
		smcb:    smcbHandle,
		frees:   alloc.frees, // bundle takes over the lifetime; alloc.Free no-ops after this
	}
	alloc.frees = nil

	c := &Client{
		bundle: bundle,
		closed: new(atomic.Bool),
	}
	c.cleanup = runtime.AddCleanup(c, clientCleanup, clientCleanupArgs{
		closed: c.closed,
		bundle: bundle,
	})
	return c, nil
}

// Ptr returns the underlying ZetaSdk_Client pointer. Callers must hold the
// Client's lock while using it — the SDK Client is not thread-safe.
func (c *Client) Ptr() unsafe.Pointer { return c.bundle.client }

// Lock / Unlock expose the wrapper's mutex so HTTPClient methods can
// serialize SDK calls without re-entering Client through the public API.
func (c *Client) Lock()   { c.mu.Lock() }
func (c *Client) Unlock() { c.mu.Unlock() }

// Close releases the SDK client and every C allocation backing its
// BuildConfig. Idempotent. The SDK MUST NOT be invoked through this client
// after Close returns.
func (c *Client) Close() {
	if c.closed.Swap(true) {
		return
	}
	c.cleanup.Stop()
	c.mu.Lock()
	defer c.mu.Unlock()
	freeClientBundle(c.bundle)
}

type clientCleanupArgs struct {
	closed *atomic.Bool
	bundle *clientBundle
}

func clientCleanup(args clientCleanupArgs) {
	if args.closed.Swap(true) {
		return
	}
	log.Printf("zeta: Client GC'd without Close — releasing late; this is a caller bug")
	freeClientBundle(args.bundle)
}

func freeClientBundle(b *clientBundle) {
	if b == nil {
		return
	}
	if b.client != nil {
		C.ZetaSdk_clearZetaClient(b.client)
		b.client = nil
	}
	if b.storage != nil {
		b.storage.Close()
		b.storage = nil
	}
	if b.logger != nil {
		b.logger.Close()
		b.logger = nil
	}
	if b.smcb != nil {
		b.smcb.Close()
		b.smcb = nil
	}
	for _, f := range slices.Backward(b.frees) {
		f()
	}
	b.frees = nil
	if hook := TestCleanupHook; hook != nil {
		hook()
	}
}
