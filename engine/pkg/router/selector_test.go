package router

import (
	"testing"

	"github.com/njtc406/emberengine/engine/pkg/actor"
	"github.com/stretchr/testify/assert"
)

func testPID(name string) *actor.PID {
	return actor.NewPID("127.0.0.1:0", "node1", 1, "svc1", "GameService", name, 1, "grpc")
}

func TestRouter_NilEndpoints_SelectReturnsNil(t *testing.T) {
	r := NewRouter(nil)
	assert.Nil(t, r.Select(testPID("S")))
}

func TestRouter_NilEndpoints_SelectByPidReturnsNil(t *testing.T) {
	r := NewRouter(nil)
	assert.Nil(t, r.SelectByPid(testPID("S"), testPID("R")))
}

func TestRouter_NilEndpoints_SelectByServiceUidReturnsNil(t *testing.T) {
	r := NewRouter(nil)
	assert.Nil(t, r.SelectByServiceUid(testPID("S"), "some-uid"))
}

func TestRouter_NilEndpoints_SelectByRuleReturnsNil(t *testing.T) {
	r := NewRouter(nil)
	assert.Nil(t, r.SelectByRule(testPID("S"), func(pid *actor.PID) bool { return true }))
}

func TestRouter_NilEndpoints_SelectByServiceTypeReturnsNil(t *testing.T) {
	r := NewRouter(nil)
	assert.Nil(t, r.SelectByServiceType(testPID("S"), 1, "GameService", "Game"))
}

func TestRouter_NilEndpoints_SelectByFilterAndChoiceReturnsNil(t *testing.T) {
	r := NewRouter(nil)
	assert.Nil(t, r.SelectByFilterAndChoice(testPID("S"),
		func(pid *actor.PID) bool { return true },
		func(pids []*actor.PID) []*actor.PID { return pids },
	))
}

func TestRouter_NilRouter_EndpointManagerReturnsNil(t *testing.T) {
	var r *Router
	assert.Nil(t, r.endpointManager())
}
