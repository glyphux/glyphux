package sdk

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/kernel"
)

// ErrScopeNotDeclared is returned by a HostAPI-scoped method whose required
// api scope the plugin's manifest did not declare — the enforcement PRD
// §8.3 requires ("a capability not declared in the manifest is not present
// in the HostAPI surface a plugin receives").
var ErrScopeNotDeclared = errors.New("sdk: scope not declared in manifest")

// ErrUnsupportedCoreVersion is returned by NewHostAPI when the running
// kernel (pkg/kernel.Version) does not satisfy the manifest's declared
// requires.core constraint (PRD §7.3, §10.2's "runtime enforcement (call
// time)"). Manifest.Validate only checks that the constraint is
// well-formed; this is the actual runtime check against the real kernel.
var ErrUnsupportedCoreVersion = errors.New("sdk: kernel version does not satisfy requires.core")

// ErrUnsupportedContract is returned by NewHostAPI when the manifest's
// requires.contract names a contract version this kernel does not know
// about (see knownContractVersions below). Manifest.Validate only checks
// that requires.contract is non-empty; this is the actual runtime check.
var ErrUnsupportedContract = errors.New("sdk: requires.contract is not a known contract version")

// knownContractVersions are the composition contract versions this running
// kernel understands well enough to grant a plugin a HostAPI.
// pkg/contract.ContentCompositionV0 is the only one that exists today (see
// pkg/contract/contract.go). This lives here, not in pkg/contract itself,
// because pkg/contract.go defines what a contract version IS; whether a
// given plugin's declared requires.contract is one this KERNEL currently
// supports is a HostAPI-construction concern, the same layering pkg/sdk
// already uses for requires.core (see pkg/kernel).
var knownContractVersions = map[string]bool{
	string(contract.ContentCompositionV0): true,
}

// hostPrincipal is the principal every Tier-A HostAPI operation runs the
// underlying kernel domain API call as. Tier A is first-party, fully
// trusted, in-process code (PRD §8.1) — the kernel's own per-user-role
// capability check (internal/permission) is a different axis entirely
// (which END USER is acting) from what this package enforces (which
// CAPABILITIES the PLUGIN PACKAGE itself declared and was granted). A
// plugin's actual gate is its manifest's declared api/permission scopes,
// checked here before the call ever reaches the kernel API.
var hostPrincipal = &permission.Principal{Role: permission.RoleAdmin}

// KernelDeps are the real kernel domain APIs a HostAPI is backed by.
type KernelDeps struct {
	Compositions *composition.Store
	Content      *content.API
	Media        *media.API
	Identities   *identity.Service

	// KV backs every plugin's Store() from this KernelDeps, namespaced
	// internally per plugin name. Shared across every HostAPI built from
	// the same KernelDeps (e.g. every plugin loaded by one running daemon),
	// which is what makes the namespacing real rather than accidental.
	// Left nil (the common case — e.g. in most tests), NewHostAPI gives
	// that single HostAPI its own private backend.
	KV *MemoryKVBackend

	// Bus backs every plugin's On/Emit from this KernelDeps — the real,
	// cross-plugin event bus (PRD §8.4, slice 2.2). Shared across every
	// HostAPI built from the same KernelDeps, exactly like KV above: that
	// sharing is what makes "an emitted event reaches every subscriber
	// across every plugin" true rather than each HostAPI dispatching only
	// to its own private subscriber list. Left nil (the common case in
	// tests that only exercise a single HostAPI's own On/Emit), NewHostAPI
	// gives that single HostAPI its own private bus.
	Bus *EventBus
}

// EventBus is the real, cross-plugin event bus underlying every HostAPI's
// On/Emit (PRD §8.4: "a single event bus underlies all tiers"). Multiple
// HostAPI instances built from the same KernelDeps.Bus share one EventBus,
// so a plugin's Emit dispatches to every subscriber across every plugin on
// the bus, not just its own handlers.
//
// Dispatch order across subscribers follows registration order (the same
// guarantee slice 2.1's stub made per-HostAPI, now true bus-wide). Handler
// errors are collected via errors.Join rather than stopping dispatch at the
// first failure: one subscriber's misbehaving handler should not silently
// prevent every other plugin on the bus from observing the event — plugins
// are mutually untrusted (PRD §10.1), so one plugin's bug should not become
// another plugin's outage. The emitting caller still learns something
// failed (a non-nil, inspectable error), it just doesn't learn "nothing ran
// after the first error" when in fact everything ran.
type EventBus struct {
	mu          sync.Mutex
	subscribers map[string][]EventHandler
}

// NewEventBus returns an empty bus.
func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[string][]EventHandler)}
}

func (b *EventBus) subscribe(event string, handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[event] = append(b.subscribers[event], handler)
}

func (b *EventBus) emit(ctx context.Context, event string, payload any) error {
	b.mu.Lock()
	handlers := append([]EventHandler(nil), b.subscribers[event]...)
	b.mu.Unlock()
	var errs []error
	for _, handler := range handlers {
		if err := handler(ctx, payload); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// sensitiveEventRule names the api capability (and, where meaningful, the
// specific scope) a manifest must declare in order to subscribe to a
// sensitive domain event via On (PRD §8.4: "subscribing to a sensitive
// event ... requires the corresponding capability scope, because the
// subscriber sees sensitive data flow"). An empty Scope means "the
// capability must be declared at all, any scope suffices" — the same
// "declared at all" gate RegisterBlock already uses for a lighter-weight
// check, used here because payments/membership have no real HostAPI method
// surface yet (see knownAPIScopes' doc comment) so there is no finer-grained
// scope to require beyond "this plugin owns this capability at all".
type sensitiveEventRule struct {
	Capability string
	Scope      string
}

// sensitiveEvents maps the PRD §8.4 domain events with real sensitive-data
// exposure to the capability a subscriber must declare. Judgment calls (see
// this slice's tracking doc for the full reasoning):
//   - user.created / user.deleted -> users:read. Observing account
//     creation/deletion is a read-grade exposure of user data, and "read" is
//     the existing users scope that already gates UsersAPI.List.
//   - order.placed / payment.completed / payment.refunded -> payments
//     (any scope). Orders and payments are treated as one sensitivity tier
//     (both are financial-transaction data) since the PRD's own capability
//     graph doesn't name a separate "orders" capability distinct from
//     "payments" (§7.4 line ~592-594).
//   - membership.started / membership.expired -> membership (any scope).
//     Kept distinct from users:read rather than folded into it, because
//     subscription/access-tier state is a different fact than account
//     identity, and the PRD's own capability graph (§7.4) names "membership"
//     as its own capability separately from "users"/"identity".
//
// Every other named event in §8.4's list (the lifecycle events:
// composition.before_build/after_build, content.before_save/after_save,
// content.published/unpublished, request.before_render/after_render, and
// plugin.loaded/plugin.error) is treated as a lower-sensitivity structural
// hook: subscribing to any of them only requires the baseline events:
// subscribe scope every On call requires, not an additional domain
// capability.
var sensitiveEvents = map[string]sensitiveEventRule{
	"user.created":       {Capability: "users", Scope: "read"},
	"user.deleted":       {Capability: "users", Scope: "read"},
	"order.placed":       {Capability: "payments"},
	"payment.completed":  {Capability: "payments"},
	"payment.refunded":   {Capability: "payments"},
	"membership.started": {Capability: "membership"},
	"membership.expired": {Capability: "membership"},
}

// MemoryKVBackend is an in-memory ScopedKV backing, namespaced per plugin
// internally so multiple plugins can share one backend without colliding.
// Process-lifetime only — persisting plugin state to a real table is
// deferred (see this slice's tracking doc); the shape here (Get/Set/Delete
// mediated, never a raw handle) is what later slices extend, not replace.
type MemoryKVBackend struct {
	mu   sync.Mutex
	data map[string][]byte
}

// NewMemoryKVBackend returns an empty backend.
func NewMemoryKVBackend() *MemoryKVBackend {
	return &MemoryKVBackend{data: make(map[string][]byte)}
}

func namespacedKey(namespace, key string) string { return namespace + "\x00" + key }

func (b *MemoryKVBackend) get(namespace, key string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.data[namespacedKey(namespace, key)]
	return v, ok
}

func (b *MemoryKVBackend) set(namespace, key string, value []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[namespacedKey(namespace, key)] = append([]byte(nil), value...)
}

func (b *MemoryKVBackend) delete(namespace, key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.data, namespacedKey(namespace, key))
}

// ScopedKV is a plugin's namespaced key-value persistence (PRD §8.3) —
// never a raw DB handle.
type ScopedKV interface {
	Get(ctx context.Context, key string) (value []byte, ok bool, err error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
}

type scopedKV struct {
	backend   *MemoryKVBackend
	namespace string
}

func (s *scopedKV) Get(ctx context.Context, key string) ([]byte, bool, error) {
	v, ok := s.backend.get(s.namespace, key)
	return v, ok, nil
}

func (s *scopedKV) Set(ctx context.Context, key string, value []byte) error {
	s.backend.set(s.namespace, key, value)
	return nil
}

func (s *scopedKV) Delete(ctx context.Context, key string) error {
	s.backend.delete(s.namespace, key)
	return nil
}

// HostAPI is the only surface a plugin can reach (PRD §8.3) — capability-
// gated so a method backed by a capability the manifest didn't declare is
// not exposed.
type HostAPI interface {
	Content() ContentAPI
	Users() UsersAPI
	Media() MediaAPI
	RegisterAdminPage(def AdminPageDef) error
	RegisterJob(def JobDef) error
	Store() ScopedKV

	// AllowsNetworkHost reports whether host is permitted by this plugin's
	// declared "network" permission allowlist — delegates to
	// Manifest.AllowsNetworkHost (see its doc comment for exact-match,
	// deny-by-default semantics). This is the decision primitive a future
	// WASM/RPC-host outbound-request wrapper (slices 2.4/2.5) is expected to
	// call before making any network call on this plugin's behalf; nothing
	// in this slice actually intercepts outbound traffic yet.
	AllowsNetworkHost(host string) bool

	// RegisterContentType and RegisterBlock are the "Composition / content
	// domain" registration calls (PRD §8.3) — structural declarations, not
	// item-level CRUD (that's ContentAPI). Gated by the "content" api
	// capability: a judgment call (the PRD sketch doesn't split this out
	// separately) that registering a content type is a content:write-grade
	// structural change, while registering a block is a lighter, read-grade
	// declaration — see this slice's tracking doc.
	RegisterContentType(ctx context.Context, name string, def contract.ContentType) error
	RegisterBlock(def BlockDef) error

	// On/Emit are the event-bus subscribe/emit surface (PRD §8.4), backed by
	// the real cross-plugin EventBus (slice 2.2; KernelDeps.Bus). On is
	// gated on events:subscribe, plus the corresponding domain capability
	// for the PRD's own examples of sensitive domain events (sensitiveEvents
	// below). Emit is gated only on events:emit — deliberately not gated
	// per-event, since emitting is the lower-risk side of the bus (see
	// Emit's doc comment and this slice's tracking doc).
	On(event string, handler EventHandler) error
	Emit(ctx context.Context, event string, payload any) error
}

// EventHandler processes an emitted event's payload.
type EventHandler func(ctx context.Context, payload any) error

// BlockDef declares a Layer-2 layout block a plugin registers (PRD §8.3,
// Phase-4 extension point). Rendering/slot mechanics belong to Phase 4;
// this only records that registration happened and enforces this slice's
// gate.
type BlockDef struct {
	Name string
}

// AdminPageDef declares an admin-UI page a plugin registers (requires the
// admin_ui permission). The rendering/routing mechanics are Phase-2/4
// concerns beyond this slice; this only records that registration happened
// and enforces the gate, which is what slice 2.1 owns.
type AdminPageDef struct {
	Slug  string
	Title string
}

// JobDef declares a scheduled background job a plugin registers (requires
// the scheduled_jobs permission). Actual scheduling/execution is a later
// slice; this only records that registration happened and enforces the
// gate.
type JobDef struct {
	Name     string
	Schedule string
}

type hostAPI struct {
	manifest    Manifest
	deps        KernelDeps
	apiScope    map[string]map[string]bool
	permissions map[string]bool
	adminPages  []AdminPageDef
	jobs        []JobDef
	blocks      []BlockDef
	bus         *EventBus
	kv          *MemoryKVBackend
}

// NewHostAPI builds the HostAPI a plugin declaring manifest receives,
// backed by deps. Returns an error if manifest itself is malformed
// (Manifest.Validate) — a plugin with an invalid manifest never gets a
// HostAPI at all.
func NewHostAPI(manifest Manifest, deps KernelDeps) (HostAPI, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	if !coreSatisfied(manifest.Requires.Core, kernel.Version) {
		return nil, fmt.Errorf("%w: kernel version %s does not satisfy %q", ErrUnsupportedCoreVersion, kernel.Version, manifest.Requires.Core)
	}
	if !knownContractVersions[manifest.Requires.Contract] {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedContract, manifest.Requires.Contract)
	}
	scopes := make(map[string]map[string]bool, len(manifest.API))
	for _, s := range manifest.API {
		set := make(map[string]bool, len(s.Scopes))
		for _, sc := range s.Scopes {
			set[sc] = true
		}
		scopes[s.Capability] = set
	}
	perms := make(map[string]bool, len(manifest.Permissions))
	for _, p := range manifest.Permissions {
		perms[p.Name] = true
	}
	kv := deps.KV
	if kv == nil {
		kv = NewMemoryKVBackend()
	}
	bus := deps.Bus
	if bus == nil {
		bus = NewEventBus()
	}
	return &hostAPI{
		manifest: manifest, deps: deps, apiScope: scopes, permissions: perms, kv: kv,
		bus: bus,
	}, nil
}

// RegisterContentType defines or updates a content type, gated on the
// declared "content" api capability's write scope.
func (h *hostAPI) RegisterContentType(ctx context.Context, name string, def contract.ContentType) error {
	if !h.hasScope("content", "write") {
		return errors.New("content:write: " + ErrScopeNotDeclared.Error())
	}
	_, err := h.deps.Compositions.DefineContentType(ctx, hostPrincipal, name, def)
	return err
}

// RegisterBlock records a Layer-2 block declaration, gated on the plugin
// having declared any "content" api capability at all (read is enough — see
// the HostAPI interface doc comment on this judgment call).
func (h *hostAPI) RegisterBlock(def BlockDef) error {
	if _, declared := h.apiScope["content"]; !declared {
		return errors.New("content: " + ErrScopeNotDeclared.Error())
	}
	h.blocks = append(h.blocks, def)
	return nil
}

// On subscribes handler to event on the shared cross-plugin EventBus
// (KernelDeps.Bus). Requires the baseline events:subscribe scope; if event
// is one of the PRD §8.4 sensitive domain events (see sensitiveEvents),
// additionally requires the corresponding domain capability — because the
// subscriber sees that event's sensitive data flow, a materially different
// risk from merely emitting (§8.4: "emitting and subscribing carry
// different risk and are scoped separately").
func (h *hostAPI) On(event string, handler EventHandler) error {
	if !h.hasScope("events", "subscribe") {
		return errors.New("events:subscribe: " + ErrScopeNotDeclared.Error())
	}
	if rule, sensitive := sensitiveEvents[event]; sensitive {
		if rule.Scope == "" {
			if _, declared := h.apiScope[rule.Capability]; !declared {
				return errors.New(rule.Capability + ": " + ErrScopeNotDeclared.Error())
			}
		} else if !h.hasScope(rule.Capability, rule.Scope) {
			return errors.New(rule.Capability + ":" + rule.Scope + ": " + ErrScopeNotDeclared.Error())
		}
	}
	h.bus.subscribe(event, handler)
	return nil
}

// Emit publishes payload to every handler subscribed to event across every
// plugin sharing this HostAPI's EventBus (KernelDeps.Bus) — not just this
// plugin's own subscriptions. Requires only the baseline events:emit scope:
// per PRD §8.4, emitting is deliberately the less-scrutinized side of the
// bus (a plugin publishing what it already knows carries less risk than a
// plugin observing what other plugins/the kernel publish), so unlike On,
// Emit carries no additional per-event capability requirement even for
// events sensitiveEvents lists — see this slice's tracking doc for the full
// reasoning. If one or more subscribers' handlers error, Emit still calls
// every remaining subscriber (see EventBus.emit's doc comment) and returns
// the collected errors via errors.Join.
func (h *hostAPI) Emit(ctx context.Context, event string, payload any) error {
	if !h.hasScope("events", "emit") {
		return errors.New("events:emit: " + ErrScopeNotDeclared.Error())
	}
	return h.bus.emit(ctx, event, payload)
}

// Store returns this plugin's namespaced key-value store — always present,
// unlike Content()/Users()/Media(), since scoped persistence carries no api
// capability of its own to declare (PRD §8.3).
func (h *hostAPI) Store() ScopedKV {
	return &scopedKV{backend: h.kv, namespace: h.manifest.Name}
}

// AllowsNetworkHost reports whether host is permitted by h's manifest's
// declared "network" permission allowlist. See Manifest.AllowsNetworkHost
// and the HostAPI interface doc comment on this method for the exact-match,
// deny-by-default semantics and what remains deferred.
func (h *hostAPI) AllowsNetworkHost(host string) bool {
	return h.manifest.AllowsNetworkHost(host)
}

// hasScope reports whether the manifest declared capability with scope.
func (h *hostAPI) hasScope(capability, scope string) bool {
	return h.apiScope[capability][scope]
}

// RegisterAdminPage records def, or denies the call if the manifest did not
// declare the admin_ui permission (PRD §8.3).
func (h *hostAPI) RegisterAdminPage(def AdminPageDef) error {
	if !h.permissions["admin_ui"] {
		return errors.New("admin_ui: " + ErrScopeNotDeclared.Error())
	}
	h.adminPages = append(h.adminPages, def)
	return nil
}

// RegisterJob records def, or denies the call if the manifest did not
// declare the scheduled_jobs permission (PRD §8.3).
func (h *hostAPI) RegisterJob(def JobDef) error {
	if !h.permissions["scheduled_jobs"] {
		return errors.New("scheduled_jobs: " + ErrScopeNotDeclared.Error())
	}
	h.jobs = append(h.jobs, def)
	return nil
}

// Content returns the plugin's scoped content surface, or nil if the
// manifest declared no "content" api capability at all.
func (h *hostAPI) Content() ContentAPI {
	if _, declared := h.apiScope["content"]; !declared {
		return nil
	}
	return &scopedContent{host: h}
}

// Users returns the plugin's scoped user-management surface, or nil if the
// manifest declared no "users" api capability at all.
func (h *hostAPI) Users() UsersAPI {
	if _, declared := h.apiScope["users"]; !declared {
		return nil
	}
	return &scopedUsers{host: h}
}

// UsersAPI is the plugin-facing scoped user-management surface (api: users:
// [scopes]) — read lists accounts; manage changes role/active state.
type UsersAPI interface {
	List(ctx context.Context) ([]*identity.User, error)
	UpdateRole(ctx context.Context, userID int64, role string) (*identity.User, error)
	Deactivate(ctx context.Context, userID int64) error
	Reactivate(ctx context.Context, userID int64) error
}

type scopedUsers struct{ host *hostAPI }

func (u *scopedUsers) requireScope(scope string) error {
	if !u.host.hasScope("users", scope) {
		return errors.New("users:" + scope + ": " + ErrScopeNotDeclared.Error())
	}
	return nil
}

func (u *scopedUsers) List(ctx context.Context) ([]*identity.User, error) {
	if err := u.requireScope("read"); err != nil {
		return nil, err
	}
	return u.host.deps.Identities.ListUsers(ctx)
}

func (u *scopedUsers) UpdateRole(ctx context.Context, userID int64, role string) (*identity.User, error) {
	if err := u.requireScope("manage"); err != nil {
		return nil, err
	}
	return u.host.deps.Identities.UpdateRole(ctx, hostPrincipal, userID, role)
}

func (u *scopedUsers) Deactivate(ctx context.Context, userID int64) error {
	if err := u.requireScope("manage"); err != nil {
		return err
	}
	return u.host.deps.Identities.Deactivate(ctx, hostPrincipal, userID)
}

func (u *scopedUsers) Reactivate(ctx context.Context, userID int64) error {
	if err := u.requireScope("manage"); err != nil {
		return err
	}
	return u.host.deps.Identities.Reactivate(ctx, hostPrincipal, userID)
}

// Media returns the plugin's scoped media surface, or nil if the manifest
// declared no "media" api capability at all.
func (h *hostAPI) Media() MediaAPI {
	if _, declared := h.apiScope["media"]; !declared {
		return nil
	}
	return &scopedMedia{host: h}
}

// MediaAPI is the plugin-facing scoped media surface (api: media: [scopes]).
type MediaAPI interface {
	Get(ctx context.Context, id string) (*media.Item, error)
	List(ctx context.Context) ([]*media.Item, error)
	Upload(ctx context.Context, filename, mimeType string, data []byte) (*media.Item, error)
	Delete(ctx context.Context, id string) error
}

type scopedMedia struct{ host *hostAPI }

func (m *scopedMedia) requireScope(scope string) error {
	if !m.host.hasScope("media", scope) {
		return errors.New("media:" + scope + ": " + ErrScopeNotDeclared.Error())
	}
	return nil
}

func (m *scopedMedia) Get(ctx context.Context, id string) (*media.Item, error) {
	if err := m.requireScope("read"); err != nil {
		return nil, err
	}
	return m.host.deps.Media.Get(ctx, id)
}

func (m *scopedMedia) List(ctx context.Context) ([]*media.Item, error) {
	if err := m.requireScope("read"); err != nil {
		return nil, err
	}
	return m.host.deps.Media.List(ctx)
}

func (m *scopedMedia) Upload(ctx context.Context, filename, mimeType string, data []byte) (*media.Item, error) {
	if err := m.requireScope("write"); err != nil {
		return nil, err
	}
	return m.host.deps.Media.Upload(ctx, hostPrincipal, filename, mimeType, data)
}

func (m *scopedMedia) Delete(ctx context.Context, id string) error {
	if err := m.requireScope("write"); err != nil {
		return err
	}
	return m.host.deps.Media.Delete(ctx, hostPrincipal, id)
}

// ContentAPI is the plugin-facing scoped content surface (api: content:
// [scopes]) — mirrors internal/content.API's shape, minus the principal
// argument (the manifest's own declared scopes are the gate here).
type ContentAPI interface {
	Get(ctx context.Context, typeName, id string) (*content.Item, error)
	List(ctx context.Context, typeName string) ([]*content.Item, error)
	Create(ctx context.Context, typeName string, data map[string]any) (*content.Item, error)
	Update(ctx context.Context, typeName, id string, data map[string]any) (*content.Item, error)
	Delete(ctx context.Context, typeName, id string) error
	Publish(ctx context.Context, typeName, id string) (*content.Item, error)
	Unpublish(ctx context.Context, typeName, id string) (*content.Item, error)
}

type scopedContent struct{ host *hostAPI }

func (c *scopedContent) requireScope(scope string) error {
	if !c.host.hasScope("content", scope) {
		return errors.New("content:" + scope + ": " + ErrScopeNotDeclared.Error())
	}
	return nil
}

func (c *scopedContent) Get(ctx context.Context, typeName, id string) (*content.Item, error) {
	if err := c.requireScope("read"); err != nil {
		return nil, err
	}
	return c.host.deps.Content.Get(ctx, hostPrincipal, typeName, id)
}

func (c *scopedContent) List(ctx context.Context, typeName string) ([]*content.Item, error) {
	if err := c.requireScope("read"); err != nil {
		return nil, err
	}
	return c.host.deps.Content.List(ctx, hostPrincipal, typeName)
}

func (c *scopedContent) Create(ctx context.Context, typeName string, data map[string]any) (*content.Item, error) {
	if err := c.requireScope("write"); err != nil {
		return nil, err
	}
	return c.host.deps.Content.Create(ctx, hostPrincipal, typeName, data)
}

func (c *scopedContent) Update(ctx context.Context, typeName, id string, data map[string]any) (*content.Item, error) {
	if err := c.requireScope("write"); err != nil {
		return nil, err
	}
	return c.host.deps.Content.Update(ctx, hostPrincipal, typeName, id, data)
}

func (c *scopedContent) Delete(ctx context.Context, typeName, id string) error {
	if err := c.requireScope("write"); err != nil {
		return err
	}
	return c.host.deps.Content.Delete(ctx, hostPrincipal, typeName, id)
}

func (c *scopedContent) Publish(ctx context.Context, typeName, id string) (*content.Item, error) {
	if err := c.requireScope("publish"); err != nil {
		return nil, err
	}
	return c.host.deps.Content.Publish(ctx, hostPrincipal, typeName, id)
}

func (c *scopedContent) Unpublish(ctx context.Context, typeName, id string) (*content.Item, error) {
	if err := c.requireScope("publish"); err != nil {
		return nil, err
	}
	return c.host.deps.Content.Unpublish(ctx, hostPrincipal, typeName, id)
}
