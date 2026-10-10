// Package httpapi wires the Echo HTTP server plus the WebSocket log stream and
// serves the embedded Dashboard (see internal/interface/webui). This package
// registers only the health / version / logs-stream routes; GB28181 routes
// arrive in Change 2+.
package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// handleMediaFile 通道媒体文件直出，供浏览器原生 video 标签播放 MP4。
// 优先取 channel 级 MediaConfig，缺省时回退到节点级配置；仅支持 file 来源，
// 通过 http.ServeContent 自动处理 Range/206/If-Range/Content-Type。
// Path: GET /v1/nodes/:id/channels/:ch/media-file
func (s *Server) handleMediaFile(c echo.Context) error {
	if s.channels == nil {
		return c.JSON(http.StatusNotImplemented, errorBody{Error: "channels not implemented"})
	}
	id, err := s.nodeIDParam(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: err.Error()})
	}
	if !s.nodeExists(c.Request().Context(), id) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "unknown node " + id.String()})
	}
	channelID := strings.TrimSpace(c.Param("ch"))
	if channelID == "" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "channel id is required"})
	}
	if _, ok := s.channels.Channel(c.Request().Context(), id, channelID); !ok {
		return c.JSON(http.StatusNotFound, errorBody{Error: "channel " + channelID + " not found"})
	}

	ctx := c.Request().Context()
	cfg, has, err := s.channels.GetChannelMedia(ctx, id, channelID)
	if err != nil {
		s.log.Error("load channel media failed", "node_id", id.String(), "channel_id", channelID, "error", err.Error())
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to open media file"})
	}
	if !has {
		cfg, has, err = s.nodes.GetMedia(ctx, id)
		if err != nil {
			s.log.Error("load node media failed", "node_id", id.String(), "error", err.Error())
			return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to open media file"})
		}
	}
	if !has {
		return c.JSON(http.StatusNotFound, errorBody{Error: "no media configured"})
	}

	cfg = cfg.Normalize()
	if cfg.Kind != model.SourceKindFile {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "media file endpoint only serves file sources"})
	}
	if cfg.Path == "" {
		return c.JSON(http.StatusNotFound, errorBody{Error: "media file not found"})
	}
	// 扩展名白名单：与前端分派逻辑一致（仅 .mp4 走原生播放），
	// 把任意文件读取收窄为任意 .mp4 文件读取。
	if strings.ToLower(filepath.Ext(cfg.Path)) != ".mp4" {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "media file endpoint only serves .mp4 files"})
	}
	cfg.Path = filepath.Clean(cfg.Path)

	f, err := os.Open(cfg.Path)
	if err != nil {
		return c.JSON(http.StatusNotFound, errorBody{Error: "media file not found"})
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return c.JSON(http.StatusNotFound, errorBody{Error: "media file not found"})
	}

	http.ServeContent(c.Response(), c.Request(), filepath.Base(cfg.Path), fi.ModTime(), f)
	return nil
}
