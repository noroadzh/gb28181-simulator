package httpapi

import (
	"strings"
	"sync"
	"testing"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// TestGenerateChannelID_Basic 验证 generateChannelID 的基础正确性：
// 空通道列表 → 生成首张 ID，通过 isValidChannelID 校验。
func TestGenerateChannelID_Basic(t *testing.T) {
	nodeID := "34020000002000000001"
	id := generateChannelID(nodeID, nil)
	if id == "" {
		t.Fatal("generateChannelID returned empty string")
	}
	if !isValidChannelID(id) {
		t.Fatalf("generateChannelID = %q, want valid 20-digit numeric", id)
	}
}

// TestGenerateChannelID_Format 验证生成 ID 的每段格式：
// 前 6 位行政码、第 7-8 位行业码 00、第 9-10 位类型码 13、第 11-20 位 10 位序号。
func TestGenerateChannelID_Format(t *testing.T) {
	nodeID := "34020000002000000001"
	id := generateChannelID(nodeID, nil)
	if len(id) != 20 {
		t.Fatalf("len(id) = %d, want 20", len(id))
	}
	// 全部为数字。
	for i, c := range id {
		if c < '0' || c > '9' {
			t.Fatalf("id[%d] = %c, want digit", i, c)
		}
	}
	// 前 6 位行政码与节点 ID 前 6 位一致。
	if id[:6] != nodeID[:6] {
		t.Fatalf("id[:6] = %q, want nodeID[:6] = %q", id[:6], nodeID[:6])
	}
	// 第 7-8 位行业码 = "00"。
	if id[6:8] != "00" {
		t.Fatalf("id[6:8] = %q, want \"00\"", id[6:8])
	}
	// 第 9-10 位类型码 = "13"（通道）。
	if id[8:10] != "13" {
		t.Fatalf("id[8:10] = %q, want \"13\"", id[8:10])
	}
	// 第 11-20 位为 10 位补零序号；首张 → 1 → "0000000001"。
	if id[10:] != "0000000001" {
		t.Fatalf("id[10:] = %q, want \"0000000001\"", id[10:])
	}
}

// TestGenerateChannelID_SecondCall 验证第二次调用序号递增。
func TestGenerateChannelID_SecondCall(t *testing.T) {
	nodeID := "34020000002000000001"
	id1 := generateChannelID(nodeID, nil)
	if !isValidChannelID(id1) {
		t.Fatalf("first id = %q, want valid 20-digit numeric", id1)
	}

	// 构造"已有 id1"的通道列表。
	ch, err := model.NewChannel(id1, "test", "", model.ChannelStatusOnline)
	if err != nil {
		t.Fatalf("NewChannel: %v", err)
	}
	existing := []model.Channel{ch}
	id2 := generateChannelID(nodeID, existing)
	if id2 == id1 {
		t.Fatalf("second id = %q, must differ from first", id2)
	}
	// id2 的序号应为 2（10 位补零 = "0000000002"）。
	if id2[10:] != "0000000002" {
		t.Fatalf("id2 seq = %q, want \"0000000002\"", id2[10:])
	}
	if !isValidChannelID(id2) {
		t.Fatalf("id2 = %q, want valid 20-digit numeric", id2)
	}
}

// TestGenerateChannelID_SkipsInvalidExisting 验证扫描已有通道时跳过不符合格式的 ID。
// 这种防御性逻辑确保错误格式的历史数据不会破坏序号计算。
func TestGenerateChannelID_SkipsInvalidExisting(t *testing.T) {
	nodeID := "34020000002000000001"

	// 构造混合通道列表：旧格式 + 错误位数 + 合规格式。
	mixed := make([]model.Channel, 0, 3)
	// 旧格式（含下划线），应被跳过。
	if c, err := model.NewChannel(nodeID+"_1", "old1", "", model.ChannelStatusOnline); err == nil {
		mixed = append(mixed, c)
	} else {
		t.Fatalf("NewChannel old1: %v", err)
	}
	// 错误的位数，应被跳过——NewChannel 不限制格式，只校验非空。
	if c, err := model.NewChannel("3402000013", "badlen", "", model.ChannelStatusOnline); err == nil {
		mixed = append(mixed, c)
	} else {
		t.Fatalf("NewChannel badlen: %v", err)
	}
	// 合规格式，应参与序号计算。
	if c, err := model.NewChannel("34020000130000000005", "ok", "", model.ChannelStatusOnline); err == nil {
		mixed = append(mixed, c)
	} else {
		t.Fatalf("NewChannel ok: %v", err)
	}

	id := generateChannelID(nodeID, mixed)
	if id[10:] != "0000000006" {
		t.Fatalf("id seq = %q, want \"0000000006\" (5+1)", id[10:])
	}
	if !isValidChannelID(id) {
		t.Fatalf("id = %q, want valid 20-digit numeric", id)
	}
}

// TestGenerateChannelID_Concurrent 模拟真实生产路径下互斥锁保护：
// N 个 goroutine 同时尝试通过临界区计算并登记新 ID，验证所有 ID 唯一
// 且都符合 GB/T 28181 格式。
//
// 这里不直接调用 handleChannelAdd（需要完整的 HTTP server + node service），
// 而是显式模拟 mutex 保护的临界区逻辑：扫描现有 ID → 计算 maxSeq → 生成新 ID → 登记。
// 该模式与 handleChannelAdd 中 s.autoIDMu 保护 generateChannelID 调用
// 的方式完全一致。
func TestGenerateChannelID_Concurrent(t *testing.T) {
	nodeID := "34020000002000000001"
	const N = 50

	var mu sync.Mutex
	registry := make(map[string]struct{})

	var wg sync.WaitGroup
	results := make(chan string, N)
	ready := make(chan struct{})

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			mu.Lock()
			// 将 registry 中的 ID 转换为 generateChannelID 所需的 model.Channel 切片。
			existing := make([]model.Channel, 0, len(registry))
			for id := range registry {
				c, err := model.NewChannel(id, "x", "", model.ChannelStatusOnline)
				if err != nil {
					mu.Unlock()
					results <- ""
					return
				}
				existing = append(existing, c)
			}
			newID := generateChannelID(nodeID, existing)
			registry[newID] = struct{}{}
			mu.Unlock()
			results <- newID
		}()
	}

	close(ready)
	wg.Wait()
	close(results)

	seen := make(map[string]struct{})
	for id := range results {
		if id == "" {
			t.Fatalf("got empty id from worker (NewChannel failure)")
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id under concurrency: %q", id)
		}
		seen[id] = struct{}{}
		if !isValidChannelID(id) {
			t.Fatalf("id = %q, want valid 20-digit numeric", id)
		}
		// 不应含下划线或其他非数字字符。
		if strings.ContainsAny(id, "_-") {
			t.Fatalf("id = %q, must not contain separators", id)
		}
	}
	if len(seen) != N {
		t.Fatalf("got %d unique ids, want %d", len(seen), N)
	}
}
