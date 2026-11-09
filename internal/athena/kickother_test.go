```go name=internal/athena/kickother_test.go url=https://github.com/SyntaxNyah/Nyathena/blob/b5e97476ed322d1c8c29ac43fc4343b7a3d7fad8/internal/athena/kickother_test.go
package athena

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// newTestClients gives each test a clean client list and restores the original
// list during cleanup.
func newTestClients(t *testing.T) {
	t.Helper()

	original := clients
	t.Cleanup(func() { clients = original })

	clients = &ClientList{
		list:       make(map[*Client]struct{}),
		uidIndex:   make(map[int]*Client),
		ipidCounts: make(map[string]int),
	}
}

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

type testConn struct {
	mu     sync.Mutex
	closed bool
}

func (c *testConn) Read(_ []byte) (int, error) { return 0, io.EOF }

func (c *testConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, net.ErrClosed
	}
	return len(p), nil
}

func (c *testConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *testConn) LocalAddr() net.Addr                { return testAddr("local") }
func (c *testConn) RemoteAddr() net.Addr               { return testAddr("remote") }
func (c *testConn) SetDeadline(_ time.Time) error      { return nil }
func (c *testConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *testConn) SetWriteDeadline(_ time.Time) error { return nil }

func (c *testConn) Closed() bool {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	return closed
}

func TestKickOtherUsesHDIDAndSkipsCaller(t *testing.T) {
	origClients := clients
	t.Cleanup(func() { clients = origClients })
	clients = &ClientList{list: make(map[*Client]struct{}), uidIndex: make(map[int]*Client), ipidCounts: make(map[string]int)}

	callerConn := &testConn{}
	sameHDIDConn := &testConn{}
	sameIPIDConn := &testConn{}
	otherConn := &testConn{}

	caller := &Client{conn: callerConn, uid: 1, ipid: "ipid-a", hdid: "hdid-a"}
	sameHDIDDifferentIPID := &Client{conn: sameHDIDConn, uid: 2, ipid: "ipid-b", hdid: "hdid-a"}
	sameIPIDDifferentHDID := &Client{conn: sameIPIDConn, uid: 3, ipid: "ipid-a", hdid: "hdid-b"}
	other := &Client{conn: otherConn, uid: 4, ipid: "ipid-c", hdid: "hdid-c"}

	clients.AddClient(caller)
	clients.AddClient(sameHDIDDifferentIPID)
	clients.AddClient(sameIPIDDifferentHDID)
	clients.AddClient(other)

	cmdKickOther(caller, nil, "")

	if !sameHDIDConn.Closed() {
		t.Fatal("expected client with matching HDID to be kicked")
	}
	if sameIPIDConn.Closed() {
		t.Fatal("did not expect client with only matching IPID to be kicked")
	}
	if otherConn.Closed() {
		t.Fatal("did not expect unrelated client to be kicked")
	}
	if callerConn.Closed() {
		t.Fatal("did not expect command caller to be kicked")
	}
}
```
