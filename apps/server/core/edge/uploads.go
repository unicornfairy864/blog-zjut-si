package edge

import (
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/storage"
)

// ServeUploads 在管理端（8641）直出本地图片，供媒体库与编辑器预览。
//
// 8640 上 /uploads 仍然 404，由宿主 nginx 直出（agent.md §2）。
func ServeUploads(root string) gin.HandlerFunc {
	store := storage.Local{Root: root}

	return func(c *gin.Context) {
		rel := strings.TrimPrefix(path.Clean("/"+c.Param("filepath")), "/")
		if rel == "" || rel == "." {
			c.Status(http.StatusNotFound)
			return
		}

		filePath, err := store.Path(rel)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		// #nosec G304 -- 路径已由 storage.Path 限制在上传根目录内。
		info, err := os.Stat(filePath)
		if err != nil || !info.Mode().IsRegular() {
			c.Status(http.StatusNotFound)
			return
		}

		// 路径含内容哈希，可长期缓存（§12）。
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Header("X-Content-Type-Options", "nosniff")
		c.File(filePath)
	}
}
