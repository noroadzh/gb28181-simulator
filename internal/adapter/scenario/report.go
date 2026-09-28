package scenario

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// RenderJSON 返回 RunReport 的格式化 JSON。
func RenderJSON(r model.RunReport) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// RenderMarkdown 返回 Markdown 格式的场景执行报告。
func RenderMarkdown(r model.RunReport) string {
	var b strings.Builder
	b.WriteString("# 场景执行报告：")
	b.WriteString(r.ScenarioName)
	b.WriteString("\n\n")

	b.WriteString("- **状态**：")
	b.WriteString(string(r.Total))
	b.WriteString("\n")
	b.WriteString("- **开始**：")
	b.WriteString(r.StartedAt.Format(time.RFC3339))
	b.WriteString("\n")
	b.WriteString("- **结束**：")
	b.WriteString(r.FinishedAt.Format(time.RFC3339))
	b.WriteString("\n")

	dur := r.FinishedAt.UnixMilli() - r.StartedAt.UnixMilli()
	if dur < 0 {
		dur = 0
	}
	b.WriteString("- **耗时**：")
	b.WriteString(formatInt64(dur))
	b.WriteString(" ms\n\n")

	b.WriteString("## 步骤汇总\n\n")
	b.WriteString("| # | Type | Status | DurationMS | Error |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for i, s := range r.Steps {
		err := strings.ReplaceAll(s.Error, "\n", " ")
		b.WriteString("| ")
		b.WriteString(formatInt64(int64(i + 1)))
		b.WriteString(" | ")
		b.WriteString(s.Type)
		b.WriteString(" | ")
		b.WriteString(string(s.Status))
		b.WriteString(" | ")
		b.WriteString(formatInt64(s.DurationMS))
		b.WriteString(" | ")
		b.WriteString(err)
		b.WriteString(" |\n")
	}
	return b.String()
}

func formatInt64(v int64) string {
	// 简单非负整数格式化，避免引入 math/big 或 strconv 依赖。
	var buf [32]byte
	n := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	i := n
	for {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			break
		}
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
