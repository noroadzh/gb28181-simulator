package app

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

func TestDialogManager_CreateGet(t *testing.T) {
	mgr := NewDialogManager(DialogManagerConfig{})
	callID := "call-1"
	d := mgr.Create(callID)
	if d.CallID != callID {
		t.Errorf("CallID = %q, want %q", d.CallID, callID)
	}
	if d.Method != "INVITE" {
		t.Errorf("Method = %q, want INVITE", d.Method)
	}
	if d.State != model.DialogCalling {
		t.Errorf("State = %v, want DialogCalling", d.State)
	}
	got, ok := mgr.Get(callID)
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.CallID != callID {
		t.Errorf("Get CallID = %q, want %q", got.CallID, callID)
	}
	_, ok = mgr.Get("missing")
	if ok {
		t.Error("Get missing: expected false")
	}
}

func TestDialogManager_Confirm(t *testing.T) {
	mgr := NewDialogManager(DialogManagerConfig{})
	callID := "call-2"
	mgr.Create(callID)
	s := model.NewSession(
		"o=- 0 0 IN IP4 127.0.0.1",
		"-",
		"c=IN IP4 127.0.0.1",
		[]model.MediaStream{{MediaType: "video", Port: 9000, Protocol: "RTP/AVP", Formats: []string{"96"}}},
		nil,
	)
	d, err := mgr.Confirm(callID, &s)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if d.State != model.DialogConfirmed {
		t.Errorf("State = %v, want DialogConfirmed", d.State)
	}
	if d.Media == nil || d.Media.Origin() != s.Origin() {
		t.Error("Media not set")
	}
}

func TestDialogManager_SetRemoteTag(t *testing.T) {
	mgr := NewDialogManager(DialogManagerConfig{})
	callID := "call-3"
	mgr.Create(callID)
	d, err := mgr.SetRemoteTag(callID, "remote-1", true)
	if err != nil {
		t.Fatalf("SetRemoteTag: %v", err)
	}
	if d.RemoteTag != "remote-1" {
		t.Errorf("RemoteTag = %q, want remote-1", d.RemoteTag)
	}
	if d.State != model.DialogConfirmed {
		t.Errorf("State = %v, want DialogConfirmed after final SetRemoteTag", d.State)
	}
}

func TestDialogManager_Terminate(t *testing.T) {
	mgr := NewDialogManager(DialogManagerConfig{})
	callID := "call-4"
	mgr.Create(callID)
	mgr.Terminate(callID)
	_, ok := mgr.Get(callID)
	if ok {
		t.Error("Terminate: dialog still present")
	}
}

func TestDialogManager_Concurrent(t *testing.T) {
	mgr := NewDialogManager(DialogManagerConfig{Timeout: 2 * time.Second})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			callID := fmt.Sprintf("call-%d", i)
			mgr.Create(callID)
			mgr.Confirm(callID, nil)
			mgr.SetRemoteTag(callID, "t", false)
			mgr.Terminate(callID)
		}(i)
	}
	wg.Wait()
	if mgr.Count() != 0 {
		t.Errorf("Count = %d, want 0", mgr.Count())
	}
}

func TestDialogManager_Expiry(t *testing.T) {
	mgr := NewDialogManager(DialogManagerConfig{Timeout: 50 * time.Millisecond})
	mgr.Create("call-5")
	time.Sleep(100 * time.Millisecond)
	_, ok := mgr.Get("call-5")
	if ok {
		t.Error("Get expired dialog: expected not found")
	}
}
