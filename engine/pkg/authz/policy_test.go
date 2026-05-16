package authz

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicySnapshot_Validate_Nil(t *testing.T) {
	var s *PolicySnapshot
	assert.Error(t, s.Validate())
}

func TestPolicySnapshot_Validate_EmptyRoles(t *testing.T) {
	s := &PolicySnapshot{Roles: map[string]*PolicyRole{}, Bindings: map[string]*PolicyBinding{}}
	assert.NoError(t, s.Validate())
}

func TestPolicySnapshot_Validate_NilRole(t *testing.T) {
	s := &PolicySnapshot{
		Roles:    map[string]*PolicyRole{"admin": nil},
		Bindings: map[string]*PolicyBinding{},
	}
	assert.Error(t, s.Validate())
}

func TestPolicySnapshot_Validate_InvalidPermission(t *testing.T) {
	s := &PolicySnapshot{
		Roles:    map[string]*PolicyRole{"bad": {Permissions: []string{"noDot"}}},
		Bindings: map[string]*PolicyBinding{},
	}
	assert.Error(t, s.Validate())
}

func TestPolicySnapshot_Validate_UnknownRoleInBinding(t *testing.T) {
	s := &PolicySnapshot{
		Roles: map[string]*PolicyRole{"game": {Permissions: []string{"*"}}},
		Bindings: map[string]*PolicyBinding{
			"b1": {ServiceTypes: []string{"svc"}, Roles: []string{"nonexistent"}},
		},
	}
	err := s.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown role")
}

func TestPolicySnapshot_Validate_NoServiceTypes(t *testing.T) {
	s := &PolicySnapshot{
		Roles: map[string]*PolicyRole{"game": {Permissions: []string{"*"}}},
		Bindings: map[string]*PolicyBinding{
			"b1": {ServiceTypes: []string{}, Roles: []string{"game"}},
		},
	}
	assert.Error(t, s.Validate())
}

func TestPolicySnapshot_Validate_NoRolesInBinding(t *testing.T) {
	s := &PolicySnapshot{
		Roles: map[string]*PolicyRole{"game": {Permissions: []string{"*"}}},
		Bindings: map[string]*PolicyBinding{
			"b1": {ServiceTypes: []string{"svc"}, Roles: []string{}},
		},
	}
	assert.Error(t, s.Validate())
}

func TestPolicySnapshot_Validate_ValidWildcard(t *testing.T) {
	s := &PolicySnapshot{
		Roles: map[string]*PolicyRole{
			"admin": {Permissions: []string{"*"}},
			"game":  {Permissions: []string{"DataService.*", "Cache.Get*"}},
		},
		Bindings: map[string]*PolicyBinding{
			"b1": {ServiceTypes: []string{"game"}, Roles: []string{"game"}},
			"b2": {ServiceTypes: []string{"admin"}, Roles: []string{"admin"}},
		},
	}
	assert.NoError(t, s.Validate())
}

func TestPolicySnapshot_Normalize_Dedup(t *testing.T) {
	s := &PolicySnapshot{
		Roles: map[string]*PolicyRole{
			"r1": {Permissions: []string{" A.B ", "A.B", "C.D"}},
		},
	}
	s.Normalize()
	assert.Equal(t, []string{"A.B", "C.D"}, s.Roles["r1"].Permissions)
}

func TestAuthorizer_ApplySnapshot(t *testing.T) {
	a := NewAuthorizer()
	a.Enable(true)

	snap := &PolicySnapshot{
		Revision: 100,
		Roles: map[string]*PolicyRole{
			"game": {Permissions: []string{"DataService.*", "CacheService.Get*"}},
		},
		Bindings: map[string]*PolicyBinding{
			"b1": {ServiceTypes: []string{"game"}, Roles: []string{"game"}},
		},
	}

	err := a.ApplySnapshot(snap)
	require.NoError(t, err)
	assert.Equal(t, int64(100), a.Revision())

	// game 可以访问 DataService
	assert.NoError(t, a.Authorize(Principal{ServiceType: "game"}, "DataService", "GetUser"))
	// game 不能访问 AdminService
	assert.Error(t, a.Authorize(Principal{ServiceType: "game"}, "AdminService", "Delete"))
}

func TestAuthorizer_ApplySnapshot_InvalidReturnsError(t *testing.T) {
	a := NewAuthorizer()
	snap := &PolicySnapshot{
		Roles: map[string]*PolicyRole{"r": {Permissions: []string{"noDot"}}},
	}
	err := a.ApplySnapshot(snap)
	assert.Error(t, err)
	// revision 不应改变
	assert.Equal(t, int64(0), a.Revision())
}

func TestAuthorizer_ApplySnapshot_Nil(t *testing.T) {
	a := NewAuthorizer()
	err := a.ApplySnapshot(nil)
	assert.Error(t, err)
}

func TestAuthorizer_Snapshot_Roundtrip(t *testing.T) {
	a := NewAuthorizer()
	snap := &PolicySnapshot{
		Revision: 42,
		Roles: map[string]*PolicyRole{
			"admin": {Permissions: []string{"*"}},
		},
		Bindings: map[string]*PolicyBinding{
			"b1": {ServiceTypes: []string{"admin"}, Roles: []string{"admin"}},
		},
	}
	require.NoError(t, a.ApplySnapshot(snap))

	got := a.Snapshot()
	assert.Equal(t, int64(42), got.Revision)
	assert.Contains(t, got.Roles, "admin")
}

// --- LocalPolicyStore tests ---

func TestLocalPolicyStore_Load(t *testing.T) {
	// 创建临时策略文件
	content := `
version: 1
revision: 2026051401
roles:
  game:
    permissions:
      - "DataService.Get*"
      - "CacheService.*"
bindings:
  b1:
    serviceTypes:
      - game
    roles:
      - game
`
	tmpFile := t.TempDir() + "/policy.yaml"
	require.NoError(t, writeFile(tmpFile, content))

	store := NewLocalPolicyStore(tmpFile)
	snap, err := store.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(2026051401), snap.Revision)
	assert.Contains(t, snap.Roles, "game")
	assert.NoError(t, snap.Validate())
}

func TestLocalPolicyStore_Load_NotFound(t *testing.T) {
	store := NewLocalPolicyStore("/nonexistent/path.yaml")
	_, err := store.Load(context.Background())
	assert.Error(t, err)
}

func TestLocalPolicyStore_Watch_ReturnsNil(t *testing.T) {
	store := NewLocalPolicyStore("any")
	ch, err := store.Watch(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, ch)
}

// --- EtcdPolicyStore tests (fake client) ---

type fakeKVClient struct {
	data     map[string][]byte
	watchCh  chan WatchEvent
	closeErr error
}

func (f *fakeKVClient) Get(_ context.Context, key string) ([]byte, int64, error) {
	v, ok := f.data[key]
	if !ok {
		return nil, 0, nil
	}
	return v, 1, nil
}

func (f *fakeKVClient) Watch(_ context.Context, _ string) (<-chan WatchEvent, error) {
	if f.watchCh == nil {
		return nil, nil
	}
	return f.watchCh, nil
}

func (f *fakeKVClient) Close() error {
	return f.closeErr
}

func TestEtcdPolicyStore_Load(t *testing.T) {
	snap := PolicySnapshot{
		Revision: 99,
		Roles:    map[string]*PolicyRole{"admin": {Permissions: []string{"*"}}},
		Bindings: map[string]*PolicyBinding{"b": {ServiceTypes: []string{"admin"}, Roles: []string{"admin"}}},
	}
	data, _ := json.Marshal(snap)

	client := &fakeKVClient{data: map[string][]byte{"/authz/snapshot": data}}
	store := NewEtcdPolicyStore(client, "/authz")

	got, err := store.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(99), got.Revision)
}

func TestEtcdPolicyStore_Load_Empty(t *testing.T) {
	client := &fakeKVClient{data: map[string][]byte{}}
	store := NewEtcdPolicyStore(client, "/authz")
	_, err := store.Load(context.Background())
	assert.Error(t, err)
}

func TestEtcdPolicyStore_Watch_PutEvent(t *testing.T) {
	snap := PolicySnapshot{
		Revision: 200,
		Roles:    map[string]*PolicyRole{"r": {Permissions: []string{"S.M"}}},
		Bindings: map[string]*PolicyBinding{"b": {ServiceTypes: []string{"t"}, Roles: []string{"r"}}},
	}
	data, _ := json.Marshal(snap)

	watchCh := make(chan WatchEvent, 1)
	client := &fakeKVClient{data: map[string][]byte{}, watchCh: watchCh}
	store := NewEtcdPolicyStore(client, "/authz")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := store.Watch(ctx)
	require.NoError(t, err)
	require.NotNil(t, ch)

	watchCh <- WatchEvent{Type: WatchEventPut, Value: data}
	evt := <-ch
	assert.Equal(t, PolicyEventUpdate, evt.Type)
	assert.Equal(t, int64(200), evt.Snapshot.Revision)
}

// --- PolicyWatcher tests ---

func TestPolicyWatcher_Start_FailClosed(t *testing.T) {
	store := NewLocalPolicyStore("/nonexistent")
	a := NewAuthorizer()
	w := NewPolicyWatcher(PolicyWatcherConfig{
		Store:      store,
		Authorizer: a,
		FailOpen:   false,
	})
	err := w.Start(context.Background())
	assert.Error(t, err)
}

func TestPolicyWatcher_Start_FailOpen(t *testing.T) {
	store := NewLocalPolicyStore("/nonexistent")
	a := NewAuthorizer()
	w := NewPolicyWatcher(PolicyWatcherConfig{
		Store:      store,
		Authorizer: a,
		FailOpen:   true,
	})
	err := w.Start(context.Background())
	assert.NoError(t, err)
	w.Stop()
}

func TestPolicyWatcher_Start_Success(t *testing.T) {
	content := `
version: 1
revision: 1
roles:
  r:
    permissions:
      - "S.*"
bindings:
  b:
    serviceTypes:
      - t
    roles:
      - r
`
	tmpFile := t.TempDir() + "/policy.yaml"
	require.NoError(t, writeFile(tmpFile, content))

	store := NewLocalPolicyStore(tmpFile)
	a := NewAuthorizer()
	a.Enable(true)

	w := NewPolicyWatcher(PolicyWatcherConfig{Store: store, Authorizer: a})
	require.NoError(t, w.Start(context.Background()))
	defer w.Stop()

	assert.Equal(t, int64(1), a.Revision())
	assert.NoError(t, a.Authorize(Principal{ServiceType: "t"}, "S", "Any"))
}

func TestPolicyWatcher_Stop_Idempotent(t *testing.T) {
	store := NewLocalPolicyStore(t.TempDir() + "/x.yaml")
	a := NewAuthorizer()
	w := NewPolicyWatcher(PolicyWatcherConfig{Store: store, Authorizer: a, FailOpen: true})
	_ = w.Start(context.Background())
	w.Stop()
	w.Stop() // should not panic
}

// helper
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
