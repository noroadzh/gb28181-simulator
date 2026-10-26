package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// talkAudioView is a ChannelView with a controllable audio bridge.
type talkAudioView struct {
	*fakeChannelView
	down  chan []byte
	onAir chan []byte
}

func newTalkAudioView() *talkAudioView {
	return &talkAudioView{
		fakeChannelView: newFakeChannelView(),
		down:            make(chan []byte, 4),
		onAir:           make(chan []byte, 4),
	}
}

func (v *talkAudioView) SubscribeTalkAudio(_ context.Context, _ model.NodeID, _, session string) (<-chan []byte, func(), error) {
	if session != "s1" {
		return nil, nil, errors.New("unknown talk session")
	}
	return v.down, func() {}, nil
}

func (v *talkAudioView) SendTalkAudio(_ context.Context, _ model.NodeID, _, _ string, payload []byte) error {
	v.onAir <- append([]byte(nil), payload...)
	return nil
}

// Tasks 2.2 and 2.3: one frame each way over the talk audio socket.
func TestTalkAudio_WebSocketCarriesBothDirections(t *testing.T) {
	nodes := newFakeNodeView()
	node := nodes.seed(t, "34020000001310000001", "127.0.0.1:15061", model.StatusOnline)
	view := newTalkAudioView()
	ts := newMediaFileServer(t, nodes, view)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"/v1/nodes/" + node.ID().String() + "/channels/ch-1/talk/audio?session=s1"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v (status %v)", wsURL, err, resp)
	}
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close() }()

	// Down-link: the device's payload reaches the browser.
	want := []byte("pcmu-payload")
	view.down <- want
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read down-link frame: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("down-link = %q, want %q", got, want)
	}

	// Up-link: the browser's frame reaches SendTalkAudio.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("write up-link frame: %v", err)
	}
	select {
	case payload := <-view.onAir:
		if string(payload) != string([]byte{0x01, 0x02, 0x03}) {
			t.Errorf("up-link = % X, want 01 02 03", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the up-link frame never reached the service")
	}
}

// Task 2.2: an unknown session is 404 before any upgrade.
func TestTalkAudio_UnknownSessionIs404(t *testing.T) {
	nodes := newFakeNodeView()
	node := nodes.seed(t, "34020000001310000001", "127.0.0.1:15061", model.StatusOnline)
	ts := newMediaFileServer(t, nodes, newTalkAudioView())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") +
		"/v1/nodes/" + node.ID().String() + "/channels/ch-1/talk/audio?session=nope"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("dialling an unknown talk session succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %v, want 404", resp)
	}
}
