// Package httpapi — 媒体文件上传 HTTP API。
//
// 提供浏览器端媒体文件（MP4/TS/MKV 等）上传到容器内的能力，
// 上传后返回容器内路径，可直接绑定为节点级或通道级媒体源。
package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

const (
	// uploadDir 是容器内上传文件存放目录（相对于数据根目录）。
	uploadDir = "uploads"
	// uploadExtWhitelist 是允许上传的媒体扩展名白名单。
	uploadExtWhitelist = ".mp4,.ts,.mkv,.flv,.h264,.h265,.avi,.mov,.webm"
	// uploadMaxBytes 是上传文件大小上限（2 GB）。
	uploadMaxBytes int64 = 2 << 30
)

// uploadResponse 是 POST /nodes/:id/media/upload 的响应格式。
type uploadResponse struct {
	Path string `json:"path"` // 容器内绝对路径，可直接填入 MediaConfig.path
	Name string `json:"name"` // 原始文件名
	Size int64  `json:"size"` // 字节数
}

// handleMediaUpload 处理媒体文件上传。
// 路径：POST /v1/nodes/:id/media/upload
// 请求：multipart/form-data，字段名 file
// 响应：{"path":"/var/lib/gb28181-simulator/uploads/xxx.mp4","name":"xxx.mp4","size":12345}
func (s *Server) handleMediaUpload(c echo.Context) error {
	nodeID, err := model.ParseNodeID(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "invalid node id"})
	}
	if !s.nodeExists(c.Request().Context(), nodeID) {
		return c.JSON(http.StatusNotFound, errorBody{Error: "node not found"})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "missing file field"})
	}
	if file.Size > uploadMaxBytes {
		return c.JSON(http.StatusRequestEntityTooLarge, errorBody{Error: fmt.Sprintf("file too large: %d bytes (max %d)", file.Size, uploadMaxBytes)})
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !strings.Contains(uploadExtWhitelist, ext) {
		return c.JSON(http.StatusBadRequest, errorBody{Error: "unsupported file type: " + ext + " (allowed: " + uploadExtWhitelist + ")"})
	}

	// 构造目标目录：<数据根目录>/uploads/
	// 数据根目录取自存储路径的父目录。
	targetDir := filepath.Join(s.uploadRoot(), uploadDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to create upload directory"})
	}

	// 文件名清洗：只保留 Base + 时间戳，防目录穿越。
	cleanName := fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), strings.TrimSuffix(filepath.Base(file.Filename), filepath.Ext(file.Filename)), ext)
	targetPath := filepath.Join(targetDir, cleanName)

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to open uploaded file"})
	}
	defer src.Close()

	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to create target file"})
	}
	defer dst.Close()

	if _, err := dst.ReadFrom(src); err != nil {
		return c.JSON(http.StatusInternalServerError, errorBody{Error: "failed to save file"})
	}

	s.log.Info("media uploaded", "node", nodeID.String(), "path", targetPath, "size", file.Size)
	return c.JSON(http.StatusOK, uploadResponse{
		Path: targetPath,
		Name: file.Filename,
		Size: file.Size,
	})
}

// uploadRoot 返回上传文件的根目录（容器内），取自 SQLite 存储路径的父目录。
func (s *Server) uploadRoot() string {
	if s.cfg.Storage.Path != "" {
		return filepath.Dir(s.cfg.Storage.Path)
	}
	return "/var/lib/gb28181-simulator"
}
