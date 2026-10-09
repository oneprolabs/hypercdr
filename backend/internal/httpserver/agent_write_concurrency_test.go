package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestAgentInventoryTaskAndAckWritesShareOneWriter(t *testing.T) {
	repo := newTestStore(t)
	cluster := testTenantCluster(t, repo, store.DefaultTenantID, "concurrent-agent")
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, req, nil)
		if err == nil {
			accepted <- conn
		}
	}))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	conn := <-accepted
	defer conn.Close()
	r := &Router{store: repo, hub: newSessionHub(), logger: slog.Default(), inventory: map[string]inventoryRequestStatus{}}
	r.hub.set(cluster.ID, conn)
	const perWriter = 20
	failures := make(chan error, perWriter*3)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, kind := range []string{"inventory", "task", "ack"} {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			<-start
			for i := 0; i < perWriter; i++ {
				switch kind {
				case "inventory":
					req := httptest.NewRequest("POST", "/api/v1/clusters/"+cluster.ID+"/inventory/request", strings.NewReader(`{}`))
					req.SetPathValue("id", cluster.ID)
					w := httptest.NewRecorder()
					r.requestClusterInventory(w, req)
					if w.Code != 202 {
						failures <- &writerStatusError{status: w.Code}
					}
				case "task":
					if err := r.dispatchStoredTask(conn, store.Task{ID: store.NewPublicID(), ClusterID: cluster.ID, Type: "unregister", Payload: map[string]any{"namespace": "hypercdr-agent"}}); err != nil {
						failures <- err
					}
				case "ack":
					if err := r.writeEventAck(conn, cluster.ID, "agent", store.NewPublicID(), "test", "", ""); err != nil {
						failures <- err
					}
				}
			}
		}(kind)
	}
	close(start)
	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	counts := map[string]int{}
	for i := 0; i < perWriter*3; i++ {
		var msg protocol.Message[map[string]any]
		if err := client.ReadJSON(&msg); err != nil {
			t.Fatal(err)
		}
		counts[msg.Type]++
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	for _, kind := range []string{protocol.MessagePlatformInventoryRequest, protocol.MessagePlatformTaskDispatch, protocol.MessagePlatformEventAck} {
		if counts[kind] != perWriter {
			t.Fatalf("lost messages: %#v", counts)
		}
	}
}

type writerStatusError struct{ status int }

func (e *writerStatusError) Error() string { return http.StatusText(e.status) }
