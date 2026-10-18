package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/buckket/go-blurhash"
	"github.com/studio-b12/gowebdav"
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/imaging"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/storage"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/task"
)

// MediaService 处理上传、变体生成、媒体库、WebDAV 归档与删除前引用检查（agent.md §10）。
type MediaService struct{}

const (
	// mediaMaxPixels 是像素上限（§6：> 5000 万拒绝，2003）。
	mediaMaxPixels int64 = 50_000_000
	// mediaAltMaxLen 是 alt 文本上限。
	mediaAltMaxLen = 200
	// mediaErrorMaxLen 是 media.error 的字符上限。
	mediaErrorMaxLen = 500
	// blurhash 组件数与缩略宽度（§10：20px 输入即可，这里用 64px 更稳）。
	mediaBlurhashComponentsX = 4
	mediaBlurhashComponentsY = 3
	mediaBlurhashThumbWidth  = 64
)

// mediaVariantTiers 是四档变体宽度（§10：480/960/1600/2400）。
var mediaVariantTiers = []int{480, 960, 1600, 2400}

// mediaProcessMu 保证转码全局串行（§6：worker 并发固定为 1）。
var mediaProcessMu sync.Mutex

// MediaUploadInput 是一次上传的入参；File 由 api 层从 multipart part 直接传入。
type MediaUploadInput struct {
	File     io.Reader
	FileName string
	Alt      string
}

// MediaUploadResult 区分新建与命中已有内容（sha256 去重）。
type MediaUploadResult struct {
	Media   *model.Media
	Created bool
}

// MediaItem 是媒体库输出；URL 由 img_base_url 与 storage_key 拼出。
type MediaItem struct {
	ID           int64             `json:"id"`
	SHA256       string            `json:"sha256"`
	OriginalName string            `json:"originalName"`
	Mime         string            `json:"mime"`
	Ext          string            `json:"ext"`
	Bytes        int64             `json:"bytes"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	Pixels       int64             `json:"pixels"`
	Alt          string            `json:"alt"`
	Blurhash     string            `json:"blurhash"`
	Status       string            `json:"status"`
	Error        string            `json:"error"`
	URL          string            `json:"url"`
	Variants     map[string]string `json:"variants"`
	CreatedAt    dbtime.Unix       `json:"createdAt"`
	UpdatedAt    dbtime.Unix       `json:"updatedAt"`
}

// mediaJobPayload 是 media_process / media_archive 的任务负载。
type mediaJobPayload struct {
	MediaID int64 `json:"mediaId"`
}

// Upload 流式接收文件、校验、去重、摆正并落盘原图，然后投递转码任务。
//
// 返回时 status 为 processing；变体生成后由调度器更新为 ready（§10）。
func (s MediaService) Upload(in MediaUploadInput, actor PostActor) (*MediaUploadResult, error) {
	root := global.Config.Upload.Dir
	maxBytes := global.Config.Upload.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 30 << 20
	}

	tempDir := filepath.Join(root, ".tmp")
	if err := os.MkdirAll(tempDir, 0o750); err != nil {
		return nil, fmt.Errorf("创建上传临时目录失败: %w", err)
	}

	temp, err := os.CreateTemp(tempDir, "upload-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(in.File, maxBytes+1))
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("接收上传失败: %w", err)
	}
	if written == 0 {
		return nil, errs.New(errs.ErrorValidation, "没有收到文件内容")
	}
	if written > maxBytes {
		return nil, errs.New(errs.ErrorImageTooLarge, "图片超过大小上限")
	}

	format, ext, mime, err := sniffImage(tempPath)
	if err != nil {
		return nil, err
	}

	config, err := probeImage(tempPath)
	if err != nil {
		return nil, errs.New(errs.ErrorValidation, "无法解析图片，请确认文件未损坏")
	}
	if config.Pixels > mediaMaxPixels {
		return nil, errs.New(errs.ErrorImageTooLarge, "图片像素超过 5000 万")
	}

	digest := hex.EncodeToString(hasher.Sum(nil))
	existing, err := model.FindMediaBySHA(global.DB, digest)
	switch {
	case err == nil:
		return &MediaUploadResult{Media: existing, Created: false}, nil
	case !errors.Is(err, model.ErrNotFound):
		return nil, fmt.Errorf("查询重复图片失败: %w", err)
	}

	mediaProcessMu.Lock()
	defer mediaProcessMu.Unlock()

	orientation := 1
	if format == "jpeg" {
		file, openErr := os.Open(tempPath)
		if openErr != nil {
			return nil, fmt.Errorf("读取图片失败: %w", openErr)
		}
		orientation = imaging.Orientation(file)
		_ = file.Close()
	}

	file, err := os.Open(tempPath)
	if err != nil {
		return nil, fmt.Errorf("读取图片失败: %w", err)
	}
	img, _, decodeErr := imaging.Decode(file)
	_ = file.Close()
	if decodeErr != nil {
		return nil, fmt.Errorf("解码图片失败: %w", decodeErr)
	}
	img = imaging.ApplyOrientation(img, orientation)

	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	key := digest[:2] + "/" + digest
	store := storage.Local{Root: root}

	// 原图按 Orientation 摆正后重新编码，EXIF（含 GPS）随之剥离（§9）。
	originalPath, err := store.Path(key + "/original." + ext)
	if err != nil {
		return nil, fmt.Errorf("生成原图路径失败: %w", err)
	}
	if err := storage.WriteAtomic(originalPath, func(w io.Writer) error {
		return imaging.EncodeOriginal(w, format, img)
	}, 0o640); err != nil {
		return nil, fmt.Errorf("保存原图失败: %w", err)
	}

	storedBytes := written
	if info, statErr := os.Stat(originalPath); statErr == nil {
		storedBytes = info.Size()
	}

	now := dbtime.Now()
	media := &model.Media{
		SHA256:        digest,
		StorageDriver: model.StorageDriverLocal,
		StorageKey:    key,
		OriginalName:  sanitizeFileName(in.FileName),
		Mime:          mime,
		Ext:           ext,
		Bytes:         storedBytes,
		Width:         width,
		Height:        height,
		Pixels:        int64(width) * int64(height),
		VariantsJSON:  "{}",
		Alt:           truncateRunes(strings.TrimSpace(in.Alt), mediaAltMaxLen),
		Status:        model.MediaStatusProcessing,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateMedia(tx, media); err != nil {
			return err
		}
		if err := enqueueMediaJob(tx, model.JobTypeMediaProcess, media.ID, now); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionMediaUpload,
			TargetType: "media",
			TargetID:   strconv.FormatInt(media.ID, 10),
			Meta:       map[string]any{"sha256": digest, "bytes": storedBytes},
			IP:         actor.IP,
		})
	}); err != nil {
		_ = os.RemoveAll(filepath.Dir(originalPath))
		if model.IsUniqueViolation(err) {
			if raced, findErr := model.FindMediaBySHA(global.DB, digest); findErr == nil {
				return &MediaUploadResult{Media: raced, Created: false}, nil
			}
		}
		return nil, fmt.Errorf("写入媒体记录失败: %w", err)
	}

	// 立刻唤醒调度器，不用等满 60s。
	task.Notify()
	return &MediaUploadResult{Media: media, Created: true}, nil
}

// Process 生成四档 WebP 与 blurhash，并把媒体置为 ready（§10 步骤 5）。
func (s MediaService) Process(mediaID int64) error {
	media, err := model.FindMediaByID(global.DB, mediaID)
	if err != nil {
		return fmt.Errorf("查询媒体失败: %w", err)
	}

	store := storage.Local{Root: global.Config.Upload.Dir}
	originalPath, err := store.Path(media.StorageKey + "/original." + media.Ext)
	if err != nil {
		return fmt.Errorf("生成原图路径失败: %w", err)
	}

	file, err := os.Open(originalPath)
	if err != nil {
		return fmt.Errorf("读取原图失败: %w", err)
	}
	// D7 之前的原图可能仍带 EXIF；读一次 Orientation 保证变体方向正确。
	orientation := 1
	if media.Ext == "jpg" || media.Ext == "jpeg" {
		orientation = imaging.Orientation(file)
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
			_ = file.Close()
			return fmt.Errorf("读取原图失败: %w", seekErr)
		}
	}
	img, _, decodeErr := imaging.Decode(file)
	_ = file.Close()
	if decodeErr != nil {
		return fmt.Errorf("解码原图失败: %w", decodeErr)
	}
	img = imaging.ApplyOrientation(img, orientation)

	mediaProcessMu.Lock()
	defer mediaProcessMu.Unlock()

	variants := make(map[string]string, len(mediaVariantTiers))
	for _, tier := range mediaVariantTiers {
		name := strconv.Itoa(tier)
		variantPath, pathErr := store.Path(media.StorageKey + "/" + name + ".webp")
		if pathErr != nil {
			return fmt.Errorf("生成变体路径失败: %w", pathErr)
		}
		resized := imaging.ResizeToWidth(img, tier)
		if err := storage.WriteAtomic(variantPath, func(w io.Writer) error {
			return imaging.EncodeWebP(w, resized)
		}, 0o640); err != nil {
			return fmt.Errorf("生成 %s 变体失败: %w", name, err)
		}
		variants[name] = name + ".webp"
	}

	blur := ""
	if thumb := imaging.ResizeToWidth(img, mediaBlurhashThumbWidth); thumb != nil {
		if encoded, blurErr := blurhash.Encode(mediaBlurhashComponentsX, mediaBlurhashComponentsY, thumb); blurErr == nil {
			blur = encoded
		}
	}

	variantsJSON, err := json.Marshal(variants)
	if err != nil {
		return fmt.Errorf("序列化变体信息失败: %w", err)
	}

	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	now := dbtime.Now()
	if err := model.UpdateMediaFields(global.DB, media.ID, map[string]any{
		"status":        model.MediaStatusReady,
		"variants_json": string(variantsJSON),
		"blurhash":      blur,
		"width":         width,
		"height":        height,
		"pixels":        int64(width) * int64(height),
		"error":         "",
		"updated_at":    now,
	}); err != nil {
		return fmt.Errorf("更新媒体记录失败: %w", err)
	}

	if global.Config.Cloudreve.Enabled {
		if err := enqueueMediaJob(global.DB, model.JobTypeMediaArchive, media.ID, now); err != nil {
			return fmt.Errorf("投递归档任务失败: %w", err)
		}
		task.Notify()
	}
	return nil
}

// Archive 把原图推送到 Cloudreve 的 WebDAV 目录，并记录归档时间（§10 步骤 6）。
func (s MediaService) Archive(mediaID int64) error {
	config := global.Config.Cloudreve
	if !config.Enabled {
		return nil
	}

	media, err := model.FindMediaByID(global.DB, mediaID)
	if err != nil {
		return fmt.Errorf("查询媒体失败: %w", err)
	}

	store := storage.Local{Root: global.Config.Upload.Dir}
	originalPath, err := store.Path(media.StorageKey + "/original." + media.Ext)
	if err != nil {
		return fmt.Errorf("生成原图路径失败: %w", err)
	}
	file, err := os.Open(originalPath)
	if err != nil {
		return fmt.Errorf("读取原图失败: %w", err)
	}
	defer func() { _ = file.Close() }()

	client := gowebdav.NewClient(config.WebDAVURL, config.Username, config.Password)
	client.SetTimeout(config.Timeout())
	if err := client.MkdirAll(config.BasePath, 0o755); err != nil {
		return fmt.Errorf("创建图床目录失败: %w", err)
	}

	remote := path.Join(config.BasePath, media.SHA256+"."+media.Ext)
	if err := client.WriteStream(remote, file, 0o644); err != nil {
		return fmt.Errorf("上传原图到图床失败: %w", err)
	}

	now := dbtime.Now()
	if err := model.UpdateMediaFields(global.DB, media.ID, map[string]any{
		"orig_archived":    true,
		"orig_archived_at": now,
		"updated_at":       now,
	}); err != nil {
		return fmt.Errorf("更新归档状态失败: %w", err)
	}

	ServiceGroupApp.Audit.Write(AuditInput{
		Action:     AuditActionMediaArchive,
		TargetType: "media",
		TargetID:   strconv.FormatInt(media.ID, 10),
		Meta:       map[string]any{"remote": remote},
	})
	return nil
}

// HandleProcess 是 media_process 的调度器入口。
func (s MediaService) HandleProcess(_ context.Context, job *model.Job) error {
	mediaID, err := parseMediaJob(job)
	if err != nil {
		return err
	}
	if err := s.Process(mediaID); err != nil {
		if job.Attempts >= job.MaxAttempts {
			_ = model.UpdateMediaFields(global.DB, mediaID, map[string]any{
				"status":     model.MediaStatusFailed,
				"error":      truncateRunes(err.Error(), mediaErrorMaxLen),
				"updated_at": dbtime.Now(),
			})
		}
		return err
	}
	return nil
}

// HandleArchive 是 media_archive 的调度器入口。
func (s MediaService) HandleArchive(_ context.Context, job *model.Job) error {
	mediaID, err := parseMediaJob(job)
	if err != nil {
		return err
	}
	return s.Archive(mediaID)
}

// List 返回媒体库分页。
func (s MediaService) List(page, pageSize int) ([]MediaItem, int64, error) {
	items, total, err := model.ListMedia(global.DB, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("查询媒体失败: %w", err)
	}

	base := s.baseURL()
	result := make([]MediaItem, 0, len(items))
	for i := range items {
		result = append(result, toMediaItem(&items[i], base))
	}
	return result, total, nil
}

// Item 把单条实体转换成媒体库输出。
func (s MediaService) Item(media *model.Media) MediaItem {
	return toMediaItem(media, s.baseURL())
}

// Delete 删除未被引用的媒体；被引用时返回 409 / 2002 与清单（§10）。
func (s MediaService) Delete(id int64, actor PostActor) error {
	media, err := model.FindMediaByID(global.DB, id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return errs.New(errs.ErrorNotFound, "图片不存在")
		}
		return fmt.Errorf("查询媒体失败: %w", err)
	}
	if media.Status == model.MediaStatusProcessing {
		return errs.New(errs.ErrorConflict, "图片正在处理中，请稍后再删除")
	}

	refs, err := model.FindMediaRefs(global.DB, media.ID, media.StorageKey)
	if err != nil {
		return fmt.Errorf("检查图片引用失败: %w", err)
	}
	if len(refs) > 0 {
		return errs.WithDetails(errs.ErrorMediaInUse, "图片仍被文章引用，不能删除", refs)
	}

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.DeleteMedia(tx, media.ID); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionMediaDelete,
			TargetType: "media",
			TargetID:   strconv.FormatInt(media.ID, 10),
			Meta:       map[string]any{"sha256": media.SHA256},
			IP:         actor.IP,
		})
	}); err != nil {
		return fmt.Errorf("删除媒体失败: %w", err)
	}

	store := storage.Local{Root: global.Config.Upload.Dir}
	if dir, pathErr := store.Path(media.StorageKey); pathErr == nil {
		_ = os.RemoveAll(dir)
	}
	return nil
}

// Retry 把媒体重新置为 processing 并投递转码任务（D8 起改为异步）。
func (s MediaService) Retry(id int64, actor PostActor) (MediaItem, error) {
	media, err := model.FindMediaByID(global.DB, id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return MediaItem{}, errs.New(errs.ErrorNotFound, "图片不存在")
		}
		return MediaItem{}, fmt.Errorf("查询媒体失败: %w", err)
	}
	if media.Status == model.MediaStatusProcessing {
		return MediaItem{}, errs.New(errs.ErrorConflict, "图片正在处理中，请稍候")
	}

	store := storage.Local{Root: global.Config.Upload.Dir}
	if originalPath, pathErr := store.Path(media.StorageKey + "/original." + media.Ext); pathErr != nil {
		return MediaItem{}, fmt.Errorf("生成原图路径失败: %w", pathErr)
	} else if _, statErr := os.Stat(originalPath); statErr != nil {
		return MediaItem{}, errs.New(errs.ErrorNotFound, "原图不存在，无法重新处理")
	}

	now := dbtime.Now()
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.UpdateMediaFields(tx, media.ID, map[string]any{
			"status":     model.MediaStatusProcessing,
			"error":      "",
			"updated_at": now,
		}); err != nil {
			return err
		}
		if err := enqueueMediaJob(tx, model.JobTypeMediaProcess, media.ID, now); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionMediaRetry,
			TargetType: "media",
			TargetID:   strconv.FormatInt(media.ID, 10),
			IP:         actor.IP,
		})
	}); err != nil {
		return MediaItem{}, fmt.Errorf("重新处理失败: %w", err)
	}
	task.Notify()

	updated, err := model.FindMediaByID(global.DB, media.ID)
	if err != nil {
		return MediaItem{}, fmt.Errorf("查询媒体失败: %w", err)
	}
	return s.Item(updated), nil
}

// baseURL 返回图片 URL 前缀；img_base_url 为空时用同源相对路径（nginx 直出）。
func (s MediaService) baseURL() string {
	base, err := ServiceGroupApp.Setting.String(model.SettingImgBaseURL)
	if err != nil {
		base = ""
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "/uploads"
	}
	return base
}

// enqueueMediaJob 投递一条媒体任务；payload 只带 mediaId，执行时再查库。
func enqueueMediaJob(db *gorm.DB, jobType string, mediaID int64, at dbtime.Unix) error {
	payload, err := json.Marshal(mediaJobPayload{MediaID: mediaID})
	if err != nil {
		return fmt.Errorf("序列化任务负载失败: %w", err)
	}
	return model.CreateJob(db, &model.Job{
		Type:        jobType,
		Payload:     string(payload),
		RunAt:       at,
		Status:      model.JobStatusPending,
		MaxAttempts: model.DefaultJobMaxAttempts,
		CreatedAt:   at,
	})
}

// parseMediaJob 解析任务负载里的 mediaId。
func parseMediaJob(job *model.Job) (int64, error) {
	var payload mediaJobPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return 0, fmt.Errorf("解析任务负载失败: %w", err)
	}
	if payload.MediaID <= 0 {
		return 0, errors.New("任务负载缺少 mediaId")
	}
	return payload.MediaID, nil
}

// toMediaItem 解析 variants_json 并拼出可访问 URL。
func toMediaItem(media *model.Media, base string) MediaItem {
	variants := map[string]string{}
	if media.VariantsJSON != "" {
		_ = json.Unmarshal([]byte(media.VariantsJSON), &variants)
	}

	urls := make(map[string]string, len(variants))
	preferred := ""
	for name, file := range variants {
		url := base + "/" + media.StorageKey + "/" + file
		urls[name] = url
		if name == "1600" || preferred == "" {
			preferred = url
		}
	}
	if preferred == "" && media.StorageKey != "" {
		// 处理中的图片没有变体，先给出 1600 的预期 URL，前端按 status 决定是否展示。
		preferred = base + "/" + media.StorageKey + "/1600.webp"
	}

	return MediaItem{
		ID:           media.ID,
		SHA256:       media.SHA256,
		OriginalName: media.OriginalName,
		Mime:         media.Mime,
		Ext:          media.Ext,
		Bytes:        media.Bytes,
		Width:        media.Width,
		Height:       media.Height,
		Pixels:       media.Pixels,
		Alt:          media.Alt,
		Blurhash:     media.Blurhash,
		Status:       media.Status,
		Error:        media.Error,
		URL:          preferred,
		Variants:     urls,
		CreatedAt:    media.CreatedAt,
		UpdatedAt:    media.UpdatedAt,
	}
}

// sniffImage 用文件头判断格式；HEIC/AVIF 明确拒绝并提示转码（§9）。
func sniffImage(path string) (format, ext, mime string, err error) {
	file, openErr := os.Open(path)
	if openErr != nil {
		return "", "", "", fmt.Errorf("读取图片失败: %w", openErr)
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, 512)
	n, readErr := io.ReadFull(file, head)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", "", "", fmt.Errorf("读取图片失败: %w", readErr)
	}
	head = head[:n]

	if isHEIF(head) {
		return "", "", "", errs.New(errs.ErrorValidation, "暂不支持 HEIC/AVIF，请先转成 JPEG 或 WebP 再上传")
	}

	contentType := http.DetectContentType(head)
	switch contentType {
	case "image/jpeg":
		return "jpeg", "jpg", contentType, nil
	case "image/png":
		return "png", "png", contentType, nil
	case "image/gif":
		return "gif", "gif", contentType, nil
	case "image/webp":
		return "webp", "webp", contentType, nil
	default:
		return "", "", "", errs.New(errs.ErrorValidation, "只支持 JPEG、PNG、WebP、GIF")
	}
}

// isHEIF 识别 HEIC/AVIF 的 ftyp brand，用于给出"请转码"的明确提示。
func isHEIF(head []byte) bool {
	if len(head) < 12 || string(head[4:8]) != "ftyp" {
		return false
	}
	switch string(head[8:12]) {
	case "heic", "heix", "hevc", "hevx", "mif1", "msf1", "avif", "avis":
		return true
	default:
		return false
	}
}

// probeImage 先只读文件头拿尺寸，避免超大图直接解码。
func probeImage(path string) (imaging.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return imaging.Config{}, err
	}
	defer func() { _ = file.Close() }()
	return imaging.Probe(file)
}

// sanitizeFileName 只保留基础文件名并截断，避免路径与超长名字入库。
func sanitizeFileName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, base)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "image"
	}
	return truncateRunes(base, 255)
}
