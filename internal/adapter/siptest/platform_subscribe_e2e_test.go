package siptest_test

import (
	"context"
	"encoding/xml"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/credstore"
	"github.com/your-org/gb28181-simulator/internal/adapter/devicereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// alarmNotifyAnswer is the wire shape of an Alarm NOTIFY.
type alarmNotifyAnswer struct {
	XMLName  xml.Name `xml:"Notify"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

// mobilePositionAnswer is the wire shape of a MobilePosition NOTIFY.
type mobilePositionAnswer struct {
	XMLName   xml.Name `xml:"Notify"`
	CmdType   string   `xml:"CmdType"`
	SN        int      `xml:"SN"`
	DeviceID  string   `xml:"DeviceID"`
	Time      string   `xml:"Time"`
	Longitude float64  `xml:"Longitude"`
	Latitude  float64  `xml:"Latitude"`
	Speed     float64  `xml:"Speed"`
}

// subHeaderValue reads one header's value from a message.
func subHeaderValue(t *testing.T, m model.Message, name string) string {
	t.Helper()
	h, ok := m.Header(name)
	if !ok {
		return ""
	}
	return h.Value()
}

// makeSubscription builds a SUBSCRIBE request with the given event.
func makeSubscription(t *testing.T, callID, event string) model.Message {
	t.Helper()
	hdrs := []model.Header{
		model.NewHeader("From", "<sip:sub@"+e2eDomain+">;tag=sub1"),
		model.NewHeader("To", "<sip:"+e2eServer+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", callID),
		model.NewHeader("CSeq", "1 SUBSCRIBE"),
		model.NewHeader("Via", "SIP/2.0/UDP "+freeAddr(t)+";branch=z9hG4bK-sub"),
		model.NewHeader("Expires", "3600"),
		model.NewHeader("Max-Forwards", "70"),
	}
	if event != "" {
		hdrs = append(hdrs, model.NewHeader("Event", event))
	}
	msg, err := model.NewRequest("SUBSCRIBE", "sip:"+e2eServer+"@"+e2eDomain, hdrs, "")
	if err != nil {
		t.Fatalf("NewRequest SUBSCRIBE: %v", err)
	}
	return msg
}

// receiveResponse reads messages until the response for callID arrives —
// the initial NOTIFY may well beat the 200 OK onto the socket.
func receiveResponse(t *testing.T, client *siptransport.PortAdapter, callID string) model.Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		msg, _, err := client.Receive(ctx)
		cancel()
		if err != nil {
			continue
		}
		if msg.StatusCode() == 0 {
			continue // a request (the NOTIFY), not the answer
		}
		if h, ok := msg.Header("Call-ID"); ok && h.Value() == callID {
			return msg
		}
	}
	t.Fatalf("no response for %s within the budget", callID)
	return model.Message{}
}

// notifyMsg is one received NOTIFY reduced to what the assertions need.
type notifyMsg struct {
	event string
	body  string
}

// receiveNOTIFY reads messages until a NOTIFY carrying ev arrives (or the
// budget is spent, which surfaces as a zero event). Non-NOTIFY messages —
// the 200 OK responses riding the same socket — are skipped.
func receiveNOTIFY(t *testing.T, client *siptransport.PortAdapter, ev string) notifyMsg {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		msg, _, err := client.Receive(ctx)
		cancel()
		if err != nil {
			continue
		}
		if !strings.EqualFold(msg.Method(), "NOTIFY") {
			// Log every non-NOTIFY to see what's arriving on the socket.
			t.Logf("  [%s] NON-NOTIFY: %s %s body=%q",
				ev, msg.Method(), func() string {
					if s := msg.StatusCode(); s > 0 {
						return fmt.Sprintf("%d", s)
					}
					return ""
				}(), msg.Body()[:min(100, len(msg.Body()))])
			continue
		}
		if subHeaderValue(t, msg, "Event") != ev {
			continue
		}
		body := msg.Body()
		t.Logf("  [%s] RECEIVED NOTIFY body-len=%d preview=%q", ev, len(body), body[:min(200, len(body))])
		return notifyMsg{event: ev, body: body}
	}
	t.Logf("  [%s] TIMEOUT waiting for NOTIFY", ev)
	return notifyMsg{}
}

// TestPlatformSubscribeEvents checks the three supported events: each
// SUBSCRIBE is answered 200, and the initial NOTIFY carries the right
// event and shape — a catalog roster, an empty alarm body, and the
// configured position.
func TestPlatformSubscribeEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	platform := platformProfile(t, e2eServer, freeAddr(t))
	platformNode := startPlatform(t, ctx, svc, accounts, platform)

	// A device is online so the catalog roster is non-empty.
	startDevice(t, ctx, svc,
		deviceRegistration(t, platformNode.Profile().Addr(), 3600, 5*time.Second))
	waitUntil(t, "the platform to record the device", 15*time.Second, func() bool {
		return len(devices.List(ctx, platformNode.ID())) == 1
	})

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	tr := siptransport.NewPortAdapter(client)
	addr := platformNode.Profile().Addr()

	// catalog: initial NOTIFY lists the roster.
	if err := tr.Send(ctx, makeSubscription(t, "sub-catalog", "catalog"), addr); err != nil {
		t.Fatalf("Send SUBSCRIBE catalog: %v", err)
	}
	resp := receiveResponse(t, tr, "sub-catalog")
	if resp.StatusCode() != 200 {
		t.Fatalf("catalog status = %d, want 200", resp.StatusCode())
	}
	notify := receiveNOTIFY(t, tr, "catalog")
	if !strings.Contains(notify.body, "<CmdType>Catalog</CmdType>") {
		t.Errorf("catalog NOTIFY missing CmdType:\n%s", notify.body)
	}
	if !strings.Contains(notify.body, e2eDeviceA) {
		t.Errorf("catalog NOTIFY missing the registered device:\n%s", notify.body)
	}

	// alarm: initial NOTIFY is empty — nothing has happened yet.
	if err := tr.Send(ctx, makeSubscription(t, "sub-alarm", "alarm"), addr); err != nil {
		t.Fatalf("Send SUBSCRIBE alarm: %v", err)
	}
	resp = receiveResponse(t, tr, "sub-alarm")
	if resp.StatusCode() != 200 {
		t.Fatalf("alarm status = %d, want 200", resp.StatusCode())
	}
	notify = receiveNOTIFY(t, tr, "alarm")
	if notify.body != "" {
		t.Errorf("initial alarm NOTIFY body = %q, want empty", notify.body)
	}

	// mobileposition: configure the position first, subscribe second.
	pos, err := model.NewPosition(121.473701, 31.230416, 0)
	if err != nil {
		t.Fatalf("NewPosition: %v", err)
	}
	if err := svc.SetPosition(ctx, platformNode.ID(), pos); err != nil {
		t.Fatalf("SetPosition: %v", err)
	}
	if err := tr.Send(ctx, makeSubscription(t, "sub-mobileposition", "mobileposition"), addr); err != nil {
		t.Fatalf("Send SUBSCRIBE mobileposition: %v", err)
	}
	resp = receiveResponse(t, tr, "sub-mobileposition")
	if resp.StatusCode() != 200 {
		t.Fatalf("mobileposition status = %d, want 200", resp.StatusCode())
	}
	notify = receiveNOTIFY(t, tr, "mobileposition")
	var mpBody mobilePositionAnswer
	if err := xml.Unmarshal([]byte(notify.body), &mpBody); err != nil {
		t.Fatalf("unmarshal mobileposition NOTIFY: %v\n%s", err, notify.body)
	}
	if mpBody.CmdType != model.CmdTypeMobilePosition {
		t.Errorf("mobileposition CmdType = %q, want %q", mpBody.CmdType, model.CmdTypeMobilePosition)
	}
	if mpBody.DeviceID != e2eServer {
		t.Errorf("mobileposition DeviceID = %q, want %q", mpBody.DeviceID, e2eServer)
	}
	if math.Abs(mpBody.Longitude-121.473701) > 1e-9 || math.Abs(mpBody.Latitude-31.230416) > 1e-9 {
		t.Errorf("mobileposition fix = (%v, %v), want the configured one", mpBody.Longitude, mpBody.Latitude)
	}

	// Unknown event is still rejected with 489.
	if err := tr.Send(ctx, makeSubscription(t, "sub-bad", "presence"), addr); err != nil {
		t.Fatalf("Send SUBSCRIBE unknown: %v", err)
	}
	resp = receiveResponse(t, tr, "sub-bad")
	if resp.StatusCode() != 489 {
		t.Errorf("unknown event status = %d, want 489", resp.StatusCode())
	}
}

// TestPlatformPushesAlarmToSubscriber triggers an alarm through the runtime
// API on a registered device; the platform forwards it as a NOTIFY to the
// alarm subscriber.
func TestPlatformPushesAlarmToSubscriber(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	platform := platformProfile(t, e2eServer, freeAddr(t))
	platformNode := startPlatform(t, ctx, svc, accounts, platform)

	deviceID := startDevice(t, ctx, svc,
		deviceRegistration(t, platformNode.Profile().Addr(), 3600, 5*time.Second))
	waitUntil(t, "the platform to record the device", 15*time.Second, func() bool {
		return len(devices.List(ctx, platformNode.ID())) == 1
	})

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	tr := siptransport.NewPortAdapter(client)
	addr := platformNode.Profile().Addr()

	// Subscribe; swallow the initial empty-body NOTIFY.
	if err := tr.Send(ctx, makeSubscription(t, "sub-alarm-push", "alarm"), addr); err != nil {
		t.Fatalf("Send SUBSCRIBE alarm: %v", err)
	}
	resp := receiveResponse(t, tr, "sub-alarm-push")
	if resp.StatusCode() != 200 {
		t.Fatalf("alarm status = %d, want 200", resp.StatusCode())
	}
	_ = receiveNOTIFY(t, tr, "alarm")

	// Trigger the alarm through the runtime API on the device.
	if _, err := svc.TriggerAlarm(ctx, deviceID, app.AlarmInput{
		Priority:    1,
		Method:      1,
		Description: "e2e alarm",
	}); err != nil {
		t.Fatalf("TriggerAlarm: %v", err)
	}

	// The alarm arrives as a NOTIFY whose body is the Alarm XML.
	notify := receiveNOTIFY(t, tr, "alarm")
	if notify.body == "" {
		t.Fatal("no alarm NOTIFY arrived within the budget")
	}
	var a alarmNotifyAnswer
	if err := xml.Unmarshal([]byte(notify.body), &a); err != nil {
		t.Fatalf("unmarshal alarm NOTIFY: %v\n%s", err, notify.body)
	}
	if a.CmdType != model.CmdTypeAlarm {
		t.Errorf("alarm CmdType = %q, want %q", a.CmdType, model.CmdTypeAlarm)
	}
	if a.DeviceID != deviceID.String() {
		t.Errorf("alarm DeviceID = %q, want %q", a.DeviceID, deviceID.String())
	}
}

// TestPlatformPushesMobilePositionToSubscriber updates the position through
// the runtime API; the mobileposition subscriber receives a NOTIFY with the
// new coordinates.
func TestPlatformPushesMobilePositionToSubscriber(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	platform := platformProfile(t, e2eServer, freeAddr(t))
	platformNode := startPlatform(t, ctx, svc, accounts, platform)

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	tr := siptransport.NewPortAdapter(client)
	addr := platformNode.Profile().Addr()

	// No position yet: the initial NOTIFY is empty.
	if err := tr.Send(ctx, makeSubscription(t, "sub-pos-push", "mobileposition"), addr); err != nil {
		t.Fatalf("Send SUBSCRIBE mobileposition: %v", err)
	}
	resp := receiveResponse(t, tr, "sub-pos-push")
	if resp.StatusCode() != 200 {
		t.Fatalf("mobileposition status = %d, want 200", resp.StatusCode())
	}
	notify := receiveNOTIFY(t, tr, "mobileposition")
	if notify.body != "" {
		t.Errorf("initial empty-position NOTIFY body = %q, want empty", notify.body)
	}

	// Push a position through the runtime API.
	pos, err := model.NewPosition(116.397428, 39.90923, 10)
	if err != nil {
		t.Fatalf("NewPosition: %v", err)
	}
	if err := svc.SetPosition(ctx, platformNode.ID(), pos); err != nil {
		t.Fatalf("SetPosition: %v", err)
	}

	notify = receiveNOTIFY(t, tr, "mobileposition")
	if notify.body == "" {
		t.Fatal("no mobileposition NOTIFY arrived within the budget")
	}
	var mpBody mobilePositionAnswer
	if err := xml.Unmarshal([]byte(notify.body), &mpBody); err != nil {
		t.Fatalf("unmarshal mobileposition NOTIFY: %v\n%s", err, notify.body)
	}
	if mpBody.CmdType != model.CmdTypeMobilePosition {
		t.Errorf("push CmdType = %q, want %q", mpBody.CmdType, model.CmdTypeMobilePosition)
	}
	if mpBody.DeviceID != e2eServer {
		t.Errorf("push DeviceID = %q, want %q", mpBody.DeviceID, e2eServer)
	}
	if math.Abs(mpBody.Longitude-116.397428) > 1e-9 || math.Abs(mpBody.Latitude-39.90923) > 1e-9 {
		t.Errorf("pushed fix = (%v, %v), want (116.397428, 39.90923)", mpBody.Longitude, mpBody.Latitude)
	}
	if mpBody.Speed != 10 {
		t.Errorf("pushed speed = %v, want 10", mpBody.Speed)
	}
}
