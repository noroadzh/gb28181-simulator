package siptest_test

import (
	"context"
	"encoding/xml"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/adapter/credstore"
	"github.com/your-org/gb28181-simulator/internal/adapter/devicereg"
	"github.com/your-org/gb28181-simulator/internal/adapter/siptransport"
	"github.com/your-org/gb28181-simulator/internal/app"
	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// platformProfileWithPolicy is platformProfile for a node that grants a
// window of its own: a short minimum makes the platform's sweeping visible
// inside a test.
func platformProfileWithPolicy(t *testing.T, id, addr string, policy model.ExpiresPolicy) model.NodeProfile {
	t.Helper()
	profile, err := model.NewNodeProfile(id, addr, e2eDomain, "acme")
	if err != nil {
		t.Fatalf("NewNodeProfile: %v", err)
	}
	serving, err := model.NewPlatformServing(e2eDomain, policy)
	if err != nil {
		t.Fatalf("NewPlatformServing: %v", err)
	}
	profile, err = profile.WithPlatformServing(serving)
	if err != nil {
		t.Fatalf("WithPlatformServing: %v", err)
	}
	return profile
}

// startPlatform creates and starts a platform node that holds one account,
// and returns it so a test can read the address it serves on.
func startPlatform(
	t *testing.T,
	ctx context.Context,
	svc *app.NodeService,
	accounts *credstore.Store,
	profile model.NodeProfile,
) model.Node {
	t.Helper()
	platformID, err := model.ParseNodeID(e2eServer)
	if err != nil {
		t.Fatalf("ParseNodeID: %v", err)
	}
	node, err := svc.Create(ctx, profile)
	if err != nil {
		t.Fatalf("Create(platform): %v", err)
	}
	cred, err := model.NewCredentials(e2eDeviceA, e2eDomain, e2ePasswd)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if err := accounts.Add(platformID, cred); err != nil {
		t.Fatalf("accounts.Add: %v", err)
	}
	if err := svc.Start(ctx, node.ID()); err != nil {
		t.Fatalf("Start(platform): %v", err)
	}
	return node
}

// startDevice creates and starts a device node that registers with the
// platform at server.
func startDevice(
	t *testing.T,
	ctx context.Context,
	svc *app.NodeService,
	reg model.Registration,
) model.NodeID {
	t.Helper()
	created, err := svc.Create(ctx, deviceProfile(t, e2eDeviceA, freeAddr(t), reg))
	if err != nil {
		t.Fatalf("Create(device): %v", err)
	}
	if err := svc.Start(ctx, created.ID()); err != nil {
		t.Fatalf("Start(device): %v", err)
	}
	return created.ID()
}

func deviceRegistration(t *testing.T, server string, expires uint32, heartbeat time.Duration) model.Registration {
	t.Helper()
	reg, err := model.NewRegistration(model.RegistrationParams{
		Server:            server,
		ServerID:          e2eServer,
		Password:          e2ePasswd,
		Expires:           expires,
		Timeout:           3 * time.Second,
		HeartbeatInterval: heartbeat,
		HeartbeatTimeout:  100 * time.Millisecond,
		Transport:         "udp",
	})
	if err != nil {
		t.Fatalf("NewRegistration: %v", err)
	}
	return reg
}

// A device that is online keeps beating, and the platform's row moves with
// it: this is the whole point of a keepalive, seen from the platform's side
// of a real socket.
func TestPlatformSeesDeviceKeepalives(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	platform := platformProfile(t, e2eServer, freeAddr(t))
	platformNode := startPlatform(t, ctx, svc, accounts, platform)
	platformID := platformNode.ID()
	startDevice(t, ctx, svc,
		deviceRegistration(t, platformNode.Profile().Addr(), 3600, 200*time.Millisecond))

	waitUntil(t, "the platform to record the device", 15*time.Second, func() bool {
		return len(devices.List(ctx, platformID)) == 1
	})
	first, _ := devices.Lookup(ctx, platformID, e2eDeviceA)

	waitUntil(t, "a keepalive to refresh the row", 15*time.Second, func() bool {
		row, ok := devices.Lookup(ctx, platformID, e2eDeviceA)
		return ok && row.LastSeenAt().After(first.LastSeenAt())
	})
	row, _ := devices.Lookup(ctx, platformID, e2eDeviceA)
	// A heartbeat is not a re-registration: the granted lifetime stands.
	if row.GrantedExpiry() != 3600 {
		t.Errorf("granted = %d after heartbeats, want 3600", row.GrantedExpiry())
	}
	if !row.ExpiresAt().Equal(first.ExpiresAt()) {
		t.Errorf("expires at moved from %s to %s on a heartbeat", first.ExpiresAt(), row.ExpiresAt())
	}
}

// catalogAnswer is the wire shape the platform's answer is read back into.
type catalogAnswer struct {
	CmdType    string `xml:"CmdType"`
	SN         int    `xml:"SN"`
	DeviceID   string `xml:"DeviceID"`
	SumNum     int    `xml:"SumNum"`
	DeviceList struct {
		Num   int `xml:"Num,attr"`
		Items []struct {
			DeviceID string `xml:"DeviceID"`
			Name     string `xml:"Name"`
			Status   string `xml:"Status"`
			Address  string `xml:"Address"`
		} `xml:"Item"`
	} `xml:"DeviceList"`
}

// A catalog query sent by hand — no simulator code on the asking side —
// comes back as a well-formed answer listing what the platform has online.
// That is the proof the bytes on the wire are the standard's, not just our
// own dialect.
func TestPlatformAnswersCatalogQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	platform := platformProfile(t, e2eServer, freeAddr(t))
	platformNode := startPlatform(t, ctx, svc, accounts, platform)
	platformID := platformNode.ID()
	startDevice(t, ctx, svc,
		deviceRegistration(t, platformNode.Profile().Addr(), 3600, 5*time.Second))
	waitUntil(t, "the platform to record the device", 15*time.Second, func() bool {
		return len(devices.List(ctx, platformID)) == 1
	})

	client, err := siptransport.New("udp://" + freeAddr(t))
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	defer func() { _ = client.Close() }()
	tr := siptransport.NewPortAdapter(client)

	body := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Notify><CmdType>Catalog</CmdType><SN>9</SN>` +
		`<DeviceID>` + e2eServer + `</DeviceID></Notify>`
	query, err := model.NewRequest("MESSAGE", "sip:"+e2eServer+"@"+e2eDomain, []model.Header{
		model.NewHeader("From", "<sip:"+e2eServer+"@"+e2eDomain+">;tag=query1"),
		model.NewHeader("To", "<sip:"+e2eServer+"@"+e2eDomain+">"),
		model.NewHeader("Call-ID", "catalog-query-1"),
		model.NewHeader("CSeq", "1 MESSAGE"),
		model.NewHeader("Content-Type", "Application/MANSCDP+XML"),
		model.NewHeader("Max-Forwards", "70"),
	}, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := tr.Send(ctx, query, platformNode.Profile().Addr()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	resp, _, err := tr.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode())
	}
	// The answer must belong to the transaction that asked.
	if callID, ok := resp.Header("Call-ID"); !ok || callID.Value() != "catalog-query-1" {
		t.Errorf("Call-ID = %v, want it echoed", callID)
	}
	var answer catalogAnswer
	if err := xml.Unmarshal([]byte(resp.Body()), &answer); err != nil {
		t.Fatalf("the answer is not readable XML: %v\n%s", err, resp.Body())
	}
	if answer.CmdType != "Catalog" {
		t.Errorf("cmd type = %q, want Catalog", answer.CmdType)
	}
	if answer.SN != 9 {
		t.Errorf("sn = %d, want the one the query carried", answer.SN)
	}
	if answer.DeviceID != e2eServer {
		t.Errorf("device id = %q, want the platform answering", answer.DeviceID)
	}
	if answer.SumNum != 1 || answer.DeviceList.Num != 1 {
		t.Fatalf("sum = %d, num = %d, want 1/1", answer.SumNum, answer.DeviceList.Num)
	}
	item := answer.DeviceList.Items[0]
	if item.DeviceID != e2eDeviceA {
		t.Errorf("item = %s, want %s", item.DeviceID, e2eDeviceA)
	}
	if item.Status != "ON" {
		t.Errorf("status = %q, want ON", item.Status)
	}
	if item.Address == "" {
		t.Error("the catalog reports no address for the device")
	}
	if !strings.Contains(resp.Body(), "<DeviceList Num=\""+strconv.Itoa(answer.SumNum)+"\">") {
		t.Errorf("the answer does not report the count as a DeviceList attribute:\n%s", resp.Body())
	}
}

// A device that stops re-registering is swept when the lifetime it was
// granted lapses: the platform must not go on reporting a silent device as
// online, however long it takes to notice.
func TestPlatformSweepsExpiredDevice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, keeper, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	// Two seconds is the shortest lifetime this platform grants, which
	// also sets the beat its sweeper runs on.
	policy, err := model.NewExpiresPolicy(2, 3600, 7200)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	platform := platformProfileWithPolicy(t, e2eServer, freeAddr(t), policy)
	platformNode := startPlatform(t, ctx, svc, accounts, platform)
	platformID := platformNode.ID()

	// A beat well inside the granted lifetime, so the device would
	// certainly have renewed had it been allowed to.
	startDevice(t, ctx, svc,
		deviceRegistration(t, platformNode.Profile().Addr(), 2, 200*time.Millisecond))
	waitUntil(t, "the platform to record the device", 15*time.Second, func() bool {
		return len(devices.List(ctx, platformID)) == 1
	})

	// Silence: no more renewals, no more heartbeats. The platform now
	// holds a registration that is already lapsing.
	if err := keeper.Close(); err != nil {
		t.Fatalf("keeper.Close: %v", err)
	}

	waitUntil(t, "the platform to sweep the silent device", 20*time.Second, func() bool {
		return len(devices.List(ctx, platformID)) == 0
	})
}

// The sweep is a background goroutine of the serving node, so it must end
// with it: a sweeper left behind would keep touching a table whose listener
// is already gone.
func TestPlatformStopEndsTheSweeper(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	accounts := credstore.New()
	devices := devicereg.New()
	svc, _, stop := platformService(t, ctx, accounts, devices)
	defer stop()

	policy, err := model.NewExpiresPolicy(2, 3600, 7200)
	if err != nil {
		t.Fatalf("NewExpiresPolicy: %v", err)
	}
	platform := platformProfileWithPolicy(t, e2eServer, freeAddr(t), policy)
	platformNode := startPlatform(t, ctx, svc, accounts, platform)
	platformID := platformNode.ID()
	startDevice(t, ctx, svc,
		deviceRegistration(t, platformNode.Profile().Addr(), 3600, 5*time.Second))
	waitUntil(t, "the platform to record the device", 15*time.Second, func() bool {
		return len(devices.List(ctx, platformID)) == 1
	})

	if err := svc.Stop(ctx, platformID); err != nil {
		t.Fatalf("Stop(platform): %v", err)
	}
	// A stopped platform holds nobody; nothing is left to sweep.
	if got := len(devices.List(ctx, platformID)); got != 0 {
		t.Errorf("table has %d rows after the platform stopped, want 0", got)
	}
}
