/*
Copyright 2012 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Tests for groupcache.

package groupcache

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"sync"
	"testing"
	"time"
	"unsafe"

	"google.golang.org/protobuf/proto"

	pb "github.com/mailgun/groupcache/v2/groupcachepb"
	"github.com/mailgun/groupcache/v2/testpb"
	"github.com/mailgun/groupcache/v2/timer"
)

var (
	once                                 sync.Once
	stringGroup, protoGroup, expireGroup Getter

	stringc = make(chan string)

	dummyCtx context.Context

	// cacheFills is the number of times stringGroup or
	// protoGroup's Getter have been called. Read using the
	// cacheFills function.
	cacheFills AtomicInt
)

const (
	stringGroupName = "string-group"
	protoGroupName  = "proto-group"
	expireGroupName = "expire-group"
	testMessageType = "google3/net/groupcache/go/test_proto.TestMessage"
	fromChan        = "from-chan"
	cacheSize       = 1 << 20
)

func testSetup() {
	stringGroup = NewGroup(stringGroupName, cacheSize, GetterFunc(func(_ context.Context, key string, dest Sink) error {
		if key == fromChan {
			key = <-stringc
		}
		cacheFills.Add(1)
		return dest.SetString("ECHO:"+key, 0)
	}), timer.Default{})

	protoGroup = NewGroup(protoGroupName, cacheSize, GetterFunc(func(_ context.Context, key string, dest Sink) error {
		if key == fromChan {
			key = <-stringc
		}
		cacheFills.Add(1)
		return dest.SetProto(&testpb.TestMessage{
			Name: *proto.String("ECHO:" + key),
			City: *proto.String("SOME-CITY"),
		}, 0)
	}), timer.Default{})

	expireGroup = NewGroup(expireGroupName, cacheSize, GetterFunc(func(_ context.Context, key string, dest Sink) error {
		cacheFills.Add(1)
		return dest.SetString("ECHO:"+key, time.Now().Add(time.Millisecond*100).UnixNano())
	}), timer.Default{})
}

// tests that a Getter's Get method is only called once with two
// outstanding callers.  This is the string variant.
func TestGetDupSuppressString(t *testing.T) {
	once.Do(testSetup)
	// Start two getters. The first should block (waiting reading
	// from stringc) and the second should latch on to the first
	// one.
	resc := make(chan string, 2)
	for i := 0; i < 2; i++ {
		go func() {
			var s string
			if err := stringGroup.Get(dummyCtx, fromChan, StringSink(&s)); err != nil {
				resc <- "ERROR:" + err.Error()
				return
			}
			resc <- s
		}()
	}

	// Wait a bit so both goroutines get merged together via
	// singleflight.
	// TODO(bradfitz): decide whether there are any non-offensive
	// debug/test hooks that could be added to singleflight to
	// make a sleep here unnecessary.
	time.Sleep(250 * time.Millisecond)

	// Unblock the first getter, which should unblock the second
	// as well.
	stringc <- "foo"

	for i := 0; i < 2; i++ {
		select {
		case v := <-resc:
			if v != "ECHO:foo" {
				t.Errorf("got %q; want %q", v, "ECHO:foo")
			}
		case <-time.After(5 * time.Second):
			t.Errorf("timeout waiting on getter #%d of 2", i+1)
		}
	}
}

// tests that a Getter's Get method is only called once with two
// outstanding callers.  This is the proto variant.
func TestGetDupSuppressProto(t *testing.T) {
	once.Do(testSetup)
	// Start two getters. The first should block (waiting reading
	// from stringc) and the second should latch on to the first
	// one.
	resc := make(chan *testpb.TestMessage, 2)
	for i := 0; i < 2; i++ {
		go func() {
			tm := new(testpb.TestMessage)
			if err := protoGroup.Get(dummyCtx, fromChan, ProtoSink(tm)); err != nil {
				tm.Name = *proto.String("ERROR:" + err.Error())
			}
			resc <- tm
		}()
	}

	// Wait a bit so both goroutines get merged together via
	// singleflight.
	// TODO(bradfitz): decide whether there are any non-offensive
	// debug/test hooks that could be added to singleflight to
	// make a sleep here unnecessary.
	time.Sleep(250 * time.Millisecond)

	// Unblock the first getter, which should unblock the second
	// as well.
	stringc <- "Fluffy"
	want := &testpb.TestMessage{
		Name: *proto.String("ECHO:Fluffy"),
		City: *proto.String("SOME-CITY"),
	}
	for i := 0; i < 2; i++ {
		select {
		case v := <-resc:
			if !proto.Equal(v, want) {
				t.Errorf(" Got: %#v\nWant: %#v", v, want)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("timeout waiting on getter #%d of 2", i+1)
		}
	}
}

func countFills(f func()) int64 {
	fills0 := cacheFills.Get()
	f()
	return cacheFills.Get() - fills0
}

func TestCaching(t *testing.T) {
	once.Do(testSetup)
	fills := countFills(func() {
		for i := 0; i < 10; i++ {
			var s string
			if err := stringGroup.Get(dummyCtx, "TestCaching-key", StringSink(&s)); err != nil {
				t.Fatal(err)
			}
		}
	})
	if fills != 1 {
		t.Errorf("expected 1 cache fill; got %d", fills)
	}
}

func TestCachingExpire(t *testing.T) {
	once.Do(testSetup)
	fills := countFills(func() {
		for i := 0; i < 3; i++ {
			var s string
			if err := expireGroup.Get(dummyCtx, "TestCachingExpire-key", StringSink(&s)); err != nil {
				t.Fatal(err)
			}
			if i == 1 {
				time.Sleep(time.Millisecond * 150)
			}
		}
	})
	if fills != 2 {
		t.Errorf("expected 2 cache fill; got %d", fills)
	}
}

func TestCacheEviction(t *testing.T) {
	once.Do(testSetup)
	testKey := "TestCacheEviction-key"
	getTestKey := func() {
		var res string
		for i := 0; i < 10; i++ {
			if err := stringGroup.Get(dummyCtx, testKey, StringSink(&res)); err != nil {
				t.Fatal(err)
			}
		}
	}
	fills := countFills(getTestKey)
	if fills != 1 {
		t.Fatalf("expected 1 cache fill; got %d", fills)
	}

	g := stringGroup.(*Group)
	evict0 := g.mainCache.nevict

	// Trash the cache with other keys.
	var bytesFlooded int64
	// cacheSize/len(testKey) is approximate
	for bytesFlooded < cacheSize+1024 {
		var res string
		key := fmt.Sprintf("dummy-key-%d", bytesFlooded)
		stringGroup.Get(dummyCtx, key, StringSink(&res))
		bytesFlooded += int64(len(key) + len(res))
	}
	evicts := g.mainCache.nevict - evict0
	if evicts <= 0 {
		t.Errorf("evicts = %v; want more than 0", evicts)
	}

	// Test that the key is gone.
	fills = countFills(getTestKey)
	if fills != 1 {
		t.Fatalf("expected 1 cache fill after cache trashing; got %d", fills)
	}
}

func TestCachePurging(t *testing.T) {
	once.Do(testSetup)
	testKey1 := "TestCachePurging-key1"
	getTestKey1 := func() {
		var res string
		for i := 0; i < 10; i++ {
			if err := stringGroup.Get(dummyCtx, testKey1, StringSink(&res)); err != nil {
				t.Fatal(err)
			}
		}
	}
	fills := countFills(getTestKey1)
	if fills != 1 {
		t.Fatalf("expected 1 cache fill; got %d", fills)
	}

	testKey2 := "TestCachePurging-key2"
	getTestKey2 := func() {
		var res string
		for i := 0; i < 10; i++ {
			if err := stringGroup.Get(dummyCtx, testKey2, StringSink(&res)); err != nil {
				t.Fatal(err)
			}
		}
	}
	fills = countFills(getTestKey2)
	if fills != 1 {
		t.Fatalf("expected 1 cache fill; got %d", fills)
	}

	g := stringGroup.(*Group)
	// Clear the cache
	g.Clear(dummyCtx)

	// Test that the keys are gone.
	fills = countFills(getTestKey1)
	if fills != 1 {
		t.Fatalf("expected 1 cache fill after cache purging; got %d", fills)
	}
	fills = countFills(getTestKey2)
	if fills != 1 {
		t.Fatalf("expected 1 cache fill after cache purging; got %d", fills)
	}
}

type fakePeer struct {
	hits int
	fail bool
}

func (p *fakePeer) Get(_ context.Context, in *pb.GetRequest, out *pb.GetResponse) error {
	p.hits++
	if p.fail {
		return errors.New("simulated error from peer")
	}
	out.Value = []byte("got:" + in.GetKey())
	return nil
}

func (p *fakePeer) Set(_ context.Context, in *pb.SetRequest) error {
	p.hits++
	if p.fail {
		return errors.New("simulated error from peer")
	}
	return nil
}

func (p *fakePeer) Remove(_ context.Context, in *pb.GetRequest) error {
	p.hits++
	if p.fail {
		return errors.New("simulated error from peer")
	}
	return nil
}

func (p *fakePeer) Clear(_ context.Context, in *pb.GetRequest) error {
	p.hits++
	if p.fail {
		return errors.New("simulated error from peer")
	}
	return nil
}

func (p *fakePeer) GetURL() string {
	return "fakePeer"
}

type fakePeers []ProtoGetter

func (p fakePeers) PickPeer(key string) (peer ProtoGetter, ok bool) {
	if len(p) == 0 {
		return
	}
	n := crc32.Checksum([]byte(key), crc32.IEEETable) % uint32(len(p))
	return p[n], p[n] != nil
}

func (p fakePeers) GetAll() []ProtoGetter {
	return p
}

// tests that peers (virtual, in-process) are hit, and how much.
func TestPeers(t *testing.T) {
	once.Do(testSetup)
	peer0 := &fakePeer{}
	peer1 := &fakePeer{}
	peer2 := &fakePeer{}
	peerList := fakePeers([]ProtoGetter{peer0, peer1, peer2, nil})
	const cacheSize = 0 // disabled
	localHits := 0
	getter := func(_ context.Context, key string, dest Sink) error {
		localHits++
		return dest.SetString("got:"+key, 0)
	}
	testGroup := newGroup("TestPeers-group", cacheSize, GetterFunc(getter), peerList, timer.Default{})
	run := func(name string, n int, wantSummary string) {
		// Reset counters
		localHits = 0
		for _, p := range []*fakePeer{peer0, peer1, peer2} {
			p.hits = 0
		}

		for i := 0; i < n; i++ {
			key := fmt.Sprintf("key-%d", i)
			want := "got:" + key
			var got string
			err := testGroup.Get(dummyCtx, key, StringSink(&got))
			if err != nil {
				t.Errorf("%s: error on key %q: %v", name, key, err)
				continue
			}
			if got != want {
				t.Errorf("%s: for key %q, got %q; want %q", name, key, got, want)
			}
		}
		summary := func() string {
			return fmt.Sprintf("localHits = %d, peers = %d %d %d", localHits, peer0.hits, peer1.hits, peer2.hits)
		}
		if got := summary(); got != wantSummary {
			t.Errorf("%s: got %q; want %q", name, got, wantSummary)
		}
	}
	resetCacheSize := func(maxBytes int64) {
		g := testGroup
		g.cacheBytes = maxBytes
		g.mainCache = cache{}
		g.hotCache = cache{}
	}

	// Base case; peers all up, with no problems.
	resetCacheSize(1 << 20)
	run("base", 200, "localHits = 49, peers = 51 49 51")

	// Verify cache was hit.  All localHits and peers are gone as the hotCache has
	// the data we need
	run("cached_base", 200, "localHits = 0, peers = 0 0 0")
	resetCacheSize(0)

	// With one of the peers being down.
	// TODO(bradfitz): on a peer number being unavailable, the
	// consistent hashing should maybe keep trying others to
	// spread the load out. Currently it fails back to local
	// execution if the first consistent-hash slot is unavailable.
	peerList[0] = nil
	run("one_peer_down", 200, "localHits = 100, peers = 0 49 51")

	// Failing peer
	peerList[0] = peer0
	peer0.fail = true
	run("peer0_failing", 200, "localHits = 100, peers = 51 49 51")
}

func TestTruncatingByteSliceTarget(t *testing.T) {
	var buf [100]byte
	s := buf[:]
	if err := stringGroup.Get(dummyCtx, "short", TruncatingByteSliceSink(&s)); err != nil {
		t.Fatal(err)
	}
	if want := "ECHO:short"; string(s) != want {
		t.Errorf("short key got %q; want %q", s, want)
	}

	s = buf[:6]
	if err := stringGroup.Get(dummyCtx, "truncated", TruncatingByteSliceSink(&s)); err != nil {
		t.Fatal(err)
	}
	if want := "ECHO:t"; string(s) != want {
		t.Errorf("truncated key got %q; want %q", s, want)
	}
}

func TestAllocatingByteSliceTarget(t *testing.T) {
	var dst []byte
	sink := AllocatingByteSliceSink(&dst)

	inBytes := []byte("some bytes")
	sink.SetBytes(inBytes, 0)
	if want := "some bytes"; string(dst) != want {
		t.Errorf("SetBytes resulted in %q; want %q", dst, want)
	}
	v, err := sink.view()
	if err != nil {
		t.Fatalf("view after SetBytes failed: %v", err)
	}
	if &inBytes[0] == &dst[0] {
		t.Error("inBytes and dst share memory")
	}
	if &inBytes[0] == &v.b[0] {
		t.Error("inBytes and view share memory")
	}
	if &dst[0] == &v.b[0] {
		t.Error("dst and view share memory")
	}
}

// orderedFlightGroup allows the caller to force the schedule of when
// orig.Do will be called.  This is useful to serialize calls such
// that singleflight cannot dedup them.
type orderedFlightGroup struct {
	mu     sync.Mutex
	stage1 chan bool
	stage2 chan bool
	orig   flightGroup
}

func (g *orderedFlightGroup) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	<-g.stage1
	<-g.stage2
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.orig.Do(key, fn)
}

func (g *orderedFlightGroup) Lock(fn func()) {
	fn()
}

// TestNoDedup tests invariants on the cache size when singleflight is
// unable to dedup calls.
func TestNoDedup(t *testing.T) {
	const testkey = "testkey"
	const testval = "testval"
	g := newGroup("testgroup", 1024, GetterFunc(func(_ context.Context, key string, dest Sink) error {
		return dest.SetString(testval, 0)
	}), nil, timer.Default{})

	orderedGroup := &orderedFlightGroup{
		stage1: make(chan bool),
		stage2: make(chan bool),
		orig:   g.loadGroup,
	}
	// Replace loadGroup with our wrapper so we can control when
	// loadGroup.Do is entered for each concurrent request.
	g.loadGroup = orderedGroup

	// Issue two idential requests concurrently.  Since the cache is
	// empty, it will miss.  Both will enter load(), but we will only
	// allow one at a time to enter singleflight.Do, so the callback
	// function will be called twice.
	resc := make(chan string, 2)
	for i := 0; i < 2; i++ {
		go func() {
			var s string
			if err := g.Get(dummyCtx, testkey, StringSink(&s)); err != nil {
				resc <- "ERROR:" + err.Error()
				return
			}
			resc <- s
		}()
	}

	// Ensure both goroutines have entered the Do routine.  This implies
	// both concurrent requests have checked the cache, found it empty,
	// and called load().
	orderedGroup.stage1 <- true
	orderedGroup.stage1 <- true
	orderedGroup.stage2 <- true
	orderedGroup.stage2 <- true

	for i := 0; i < 2; i++ {
		if s := <-resc; s != testval {
			t.Errorf("result is %s want %s", s, testval)
		}
	}

	const wantItems = 1
	if g.mainCache.items() != wantItems {
		t.Errorf("mainCache has %d items, want %d", g.mainCache.items(), wantItems)
	}

	// If the singleflight callback doesn't double-check the cache again
	// upon entry, we would increment nbytes twice but the entry would
	// only be in the cache once.
	const wantBytes = int64(len(testkey) + len(testval))
	if g.mainCache.nbytes != wantBytes {
		t.Errorf("cache has %d bytes, want %d", g.mainCache.nbytes, wantBytes)
	}
}

func TestGroupStatsAlignment(t *testing.T) {
	var g Group
	off := unsafe.Offsetof(g.Stats)
	if off%8 != 0 {
		t.Fatal("Stats structure is not 8-byte aligned.")
	}
}

type slowPeer struct {
	fakePeer
}

func (p *slowPeer) Get(_ context.Context, in *pb.GetRequest, out *pb.GetResponse) error {
	time.Sleep(time.Second)
	out.Value = []byte("got:" + in.GetKey())
	return nil
}

func TestContextDeadlineOnPeer(t *testing.T) {
	once.Do(testSetup)
	peer0 := &slowPeer{}
	peer1 := &slowPeer{}
	peer2 := &slowPeer{}
	peerList := fakePeers([]ProtoGetter{peer0, peer1, peer2, nil})
	getter := func(_ context.Context, key string, dest Sink) error {
		return dest.SetString("got:"+key, 0)
	}
	testGroup := newGroup("TestContextDeadlineOnPeer-group", cacheSize, GetterFunc(getter), peerList, timer.Default{})

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*300)
	defer cancel()

	var got string
	err := testGroup.Get(ctx, "test-key", StringSink(&got))
	if err != nil {
		if err != context.DeadlineExceeded {
			t.Errorf("expected Get to return context deadline exceeded")
		}
	}
}

func TestCacheRemovesBytesOfReplacedValues(t *testing.T) {
	c := cache{}

	const key = "test"
	keyLen := len(key)

	for _, bytes := range []int{100, 1000, 2000} {
		c.add(key, ByteView{b: make([]byte, bytes)})
		expected := int64(bytes + keyLen)
		if c.nbytes != expected {
			t.Fatalf("%s: expected %d was %d", t.Name(), expected, c.nbytes)
		}
	}
}
