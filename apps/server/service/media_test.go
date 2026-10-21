// media.go / storage.go 的单元测试（任务 D18）。
//
// 为什么全部用例都串行、每个用例各起一套环境：global.DB / global.Config / global.Log 都是
// 进程级单例（见 main_test.go），并行会互相踩数据，所以这里不写 t.Parallel()。
// 图片一律用标准库现场编码（不读外部素材、不联网），临时目录是真实文件系统，可随意读写。
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gen2brain/webp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/imaging"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
)

// ---------------------------------------------------------------------------
// 用例内的小工具（不碰 main_test.go / setting_test.go 里已有的共享夹具）
// ---------------------------------------------------------------------------

// testPNG 现场编码一张 w×h 的 PNG：像素带渐变，避免"全同色被编码器压成极小文件"影响字节断言。
func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, gradientImage(width, height)))
	return buf.Bytes()
}

// testJPEG / testGIF / testWebP 现场编码其它受支持格式（GIF 只留首帧，与本站契约一致）。
func testJPEG(t *testing.T, width, height int) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, gradientImage(width, height), &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}

func testGIF(t *testing.T, width, height int) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, gif.Encode(&buf, gradientImage(width, height), nil))
	return buf.Bytes()
}

func testWebP(t *testing.T, width, height int) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, webp.Encode(&buf, gradientImage(width, height), webp.Options{Quality: 90}))
	return buf.Bytes()
}

// gradientImage 造一张带渐变的图：避免"全同色被编码器压成极小文件"影响字节断言。
func gradientImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 31), G: uint8(y * 17), B: 0x40, A: 0xff})
		}
	}
	return img
}

// uploadMedia 走一遍真实上传；失败直接终止用例。
func uploadMedia(t *testing.T, svc MediaService, actor PostActor, data []byte, name, alt string) *MediaUploadResult {
	t.Helper()

	result, err := svc.Upload(MediaUploadInput{
		File:     bytes.NewReader(data),
		FileName: name,
		Alt:      alt,
	}, actor)
	require.NoError(t, err)
	require.NotNil(t, result.Media)
	return result
}

// findMedia 重新读一遍媒体记录，用来断言落库后的状态。
func findMedia(t *testing.T, id int64) *model.Media {
	t.Helper()

	media, err := model.FindMediaByID(global.DB, id)
	require.NoError(t, err)
	return media
}

// uploadFilePath / fileFilePath 把 storage_key 还原成磁盘绝对路径。
func uploadFilePath(rel string) string {
	return filepath.Join(global.Config.Upload.Dir, filepath.FromSlash(rel))
}

func fileFilePath(rel string) string {
	return filepath.Join(global.Config.File.Dir, filepath.FromSlash(rel))
}

// writeTestFile 造磁盘文件（顺带建目录）。
func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, data, 0o640))
}

// probeImageFile 读回图片的真实尺寸（也顺带验证文件确实是一张能解码的图）。
func probeImageFile(t *testing.T, path string) imaging.Config {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()

	config, err := imaging.Probe(file)
	require.NoError(t, err)
	return config
}

// fourVariantFiles 是四档变体的标准文件名映射（480/960/1600/2400）。
func fourVariantFiles() map[string]string {
	files := make(map[string]string, len(mediaVariantTiers))
	for _, tier := range mediaVariantTiers {
		name := strconv.Itoa(tier)
		files[name] = name + ".webp"
	}
	return files
}

// mediaRecordOptions 是直插媒体记录的可选字段；零值即"一张 1200×800 的 ready PNG"。
type mediaRecordOptions struct {
	Ext          string
	Width        int
	Height       int
	Variants     map[string]string
	Alt          string
	Status       string
	OrigArchived bool
}

// insertMediaRecord 直插一条媒体记录（绕开 Upload），给读路径与存储对账用例造库内数据。
//
// sha256 由 key 推导，保证唯一索引不冲突；storage_key 用调用方给的完整相对路径。
func insertMediaRecord(t *testing.T, key string, opts mediaRecordOptions) *model.Media {
	t.Helper()

	if opts.Ext == "" {
		opts.Ext = "png"
	}
	if opts.Width == 0 {
		opts.Width = 1200
	}
	if opts.Height == 0 {
		opts.Height = 800
	}
	if opts.Alt == "" {
		opts.Alt = "库里图片"
	}
	if opts.Status == "" {
		opts.Status = model.MediaStatusReady
	}

	variantsJSON := "{}"
	if len(opts.Variants) > 0 {
		buf, err := json.Marshal(opts.Variants)
		require.NoError(t, err)
		variantsJSON = string(buf)
	}

	now := dbtime.Now()
	media := &model.Media{
		SHA256:        fmt.Sprintf("%x", sha256.Sum256([]byte(key))),
		StorageDriver: model.StorageDriverLocal,
		StorageKey:    key,
		OriginalName:  "seed." + opts.Ext,
		Mime:          "image/" + opts.Ext,
		Ext:           opts.Ext,
		Bytes:         1024,
		Width:         opts.Width,
		Height:        opts.Height,
		Pixels:        int64(opts.Width) * int64(opts.Height),
		VariantsJSON:  variantsJSON,
		Blurhash:      "LEHV6nWB2yk8pyo0adR*.7kCMdnj",
		Alt:           opts.Alt,
		Status:        opts.Status,
		OrigArchived:  opts.OrigArchived,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	require.NoError(t, model.CreateMedia(global.DB, media))
	require.NotZero(t, media.ID)
	return media
}

// insertAttachmentRecord 直插一条"本地上传"的附件记录（存储对账用）。
func insertAttachmentRecord(t *testing.T, key string) *model.PostAttachment {
	t.Helper()

	item := &model.PostAttachment{
		Title:      "附件",
		FileName:   "a.zip",
		StorageKey: key,
		Bytes:      1,
		SHA256:     fmt.Sprintf("%x", sha256.Sum256([]byte(key))),
		CreatedAt:  dbtime.Now(),
	}
	require.NoError(t, global.DB.Create(item).Error)
	return item
}

// mediaKeyFor 造一个合法的 storage_key：{sha[:2]}/{sha256}。
func mediaKeyFor(seed string) string {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(seed)))
	return digest[:2] + "/" + digest
}

// ---------------------------------------------------------------------------
// Upload
// ---------------------------------------------------------------------------

func TestMediaService_UploadPNGStoresOriginalAndDeduplicates(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	data := testPNG(t, 8, 8)
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	key := digest[:2] + "/" + digest

	result, err := svc.Upload(MediaUploadInput{
		File:     bytes.NewReader(data),
		FileName: "相 片.png",
		Alt:      "  测试图  ",
	}, actor)
	require.NoError(t, err)
	require.True(t, result.Created, "首次上传应当新建记录")

	media := result.Media
	assert.Equal(t, digest, media.SHA256)
	assert.Equal(t, model.StorageDriverLocal, media.StorageDriver)
	assert.Equal(t, key, media.StorageKey)
	assert.Equal(t, "png", media.Ext)
	assert.Equal(t, "image/png", media.Mime)
	assert.Equal(t, "相 片.png", media.OriginalName)
	assert.Equal(t, "测试图", media.Alt, "alt 前后空白应当被去掉")
	assert.Equal(t, 8, media.Width)
	assert.Equal(t, 8, media.Height)
	assert.Equal(t, int64(64), media.Pixels)
	// 上传返回时还没转码：状态是 processing，变体为空（§10）。
	assert.Equal(t, model.MediaStatusProcessing, media.Status)
	assert.Equal(t, "{}", media.VariantsJSON)

	// 原图真的落盘了，且入库字节数就是重新编码后的文件大小。
	originalPath := uploadFilePath(key + "/original.png")
	info, statErr := os.Stat(originalPath)
	require.NoError(t, statErr, "原图应当落盘")
	assert.Equal(t, info.Size(), media.Bytes)

	// 上传即投递一条 media_process 任务，并写一条审计。
	assert.Equal(t, int64(1), countRows(t, "jobs", "type = ? AND status = ?",
		model.JobTypeMediaProcess, model.JobStatusPending))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionMediaUpload))

	// 还没转码时 Item 先给出 1600 的预期 URL，前端按 status 决定是否展示。
	item := svc.Item(media)
	assert.Equal(t, "/uploads/"+key+"/1600.webp", item.URL)
	assert.Equal(t, "/uploads/"+key+"/original.png", item.OrigURL)
	assert.Empty(t, item.Variants)
	assert.Equal(t, digest, item.SHA256)

	// 同一张图重复上传：按 sha256 命中已有记录，返回 Created=false，不新建、不覆盖。
	again, err := svc.Upload(MediaUploadInput{
		File:     bytes.NewReader(data),
		FileName: "副本.png",
		Alt:      "另一个 alt",
	}, actor)
	require.NoError(t, err)
	assert.False(t, again.Created)
	assert.Equal(t, media.ID, again.Media.ID)
	assert.Equal(t, "相 片.png", again.Media.OriginalName, "命中去重时不应改写已有记录")
	assert.Equal(t, int64(1), countRows(t, "media", ""))
	assert.Equal(t, int64(1), countRows(t, "jobs", "type = ?", model.JobTypeMediaProcess),
		"去重命中不应重复投递转码任务")
}

func TestMediaService_UploadRejectsOversizedPixelCount(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	// 只改 IHDR 的宽高并重算 CRC：探测阶段就能判出 1 亿像素 > 5000 万上限，
	// 不需要真的解码像素（这是拒绝"解压炸弹"的关键防线）。
	data := testPNG(t, 8, 8)
	binary.BigEndian.PutUint32(data[16:20], 10000)
	binary.BigEndian.PutUint32(data[20:24], 10000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))

	_, err := svc.Upload(MediaUploadInput{File: bytes.NewReader(data), FileName: "huge.png"}, actor)
	appErr, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorImageTooLarge, appErr.Code)
	assert.Contains(t, appErr.Message, "5000 万")
	assert.Equal(t, int64(0), countRows(t, "media", ""))
}

func TestMediaService_UploadRejectsCorruptPixelData(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	// 签名与 IHDR 完好（嗅探与尺寸探测都能过），但像素数据被截断：解码阶段必须报错。
	data := testPNG(t, 8, 8)[:33]

	_, err := svc.Upload(MediaUploadInput{File: bytes.NewReader(data), FileName: "truncated.png"}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "解码图片失败")
	assert.Equal(t, int64(0), countRows(t, "media", ""))
}

func TestMediaService_UploadFailsWhenOriginalPathBlocked(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	data := testPNG(t, 8, 8)
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	key := digest[:2] + "/" + digest

	// 让 {upload.dir}/{key} 的位置被一个普通文件占住：建目录失败 → 原图写不下去。
	writeTestFile(t, uploadFilePath(key), []byte("not a directory"))

	_, err := svc.Upload(MediaUploadInput{File: bytes.NewReader(data), FileName: "blocked.png"}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "保存原图失败")
	assert.Equal(t, int64(0), countRows(t, "media", ""), "原图没落盘就不该留下媒体记录")
}

func TestMediaService_ProcessFailsOnCorruptOrMissingMedia(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	// 记录不存在。
	require.Error(t, svc.Process(987654))

	// 记录在、原图也在，但内容不是图片：解码失败，状态保持 processing 交给调度器处置。
	key := mediaKeyFor("corrupt-original")
	media := insertMediaRecord(t, key, mediaRecordOptions{Status: model.MediaStatusProcessing})
	writeTestFile(t, uploadFilePath(key+"/original.png"), []byte("这不是一张图片"))

	err := svc.Process(media.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "解码原图失败")
	assert.Equal(t, model.MediaStatusProcessing, findMedia(t, media.ID).Status)
}

func TestMediaService_UploadDedupKeepsFailedStatus(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	data := testPNG(t, 8, 8)
	media := uploadMedia(t, svc, actor, data, "a.png", "").Media
	require.NoError(t, model.UpdateMediaFields(global.DB, media.ID, map[string]any{
		"status": model.MediaStatusFailed,
		"error":  "解码原图失败",
	}))

	// 疑似缺陷：sha256 命中就直接返回旧记录，即使旧记录是 failed 也不会重新投递转码任务，
	// 于是"把失败的图删掉再传一次同一张图"救不回它（只能去管理端点 Retry）。
	again, err := svc.Upload(MediaUploadInput{File: bytes.NewReader(data), FileName: "a.png"}, actor)
	require.NoError(t, err)
	assert.False(t, again.Created)
	assert.Equal(t, model.MediaStatusFailed, again.Media.Status)
	assert.Equal(t, "解码原图失败", again.Media.Error)
	assert.Equal(t, int64(1), countRows(t, "jobs", "type = ?", model.JobTypeMediaProcess),
		"命中去重不会为失败的旧记录补投任务")
}

func TestMediaService_UploadSanitizesNameAndAlt(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	// 文件名只取基础名，控制字符被剔除；alt 按 200 个字符截断。
	longAlt := strings.Repeat("字", mediaAltMaxLen+20)
	result := uploadMedia(t, svc, actor, testPNG(t, 8, 8), "../../秘密/\x01图 片.png", longAlt)

	assert.Equal(t, "图 片.png", result.Media.OriginalName)
	assert.Equal(t, mediaAltMaxLen, utf8.RuneCountInString(result.Media.Alt))

	// 文件名为空（或只有 "."）时兜底成 "image"。
	empty := uploadMedia(t, svc, actor, testPNG(t, 9, 9), "", "")
	assert.Equal(t, "image", empty.Media.OriginalName)
}

func TestMediaService_UploadAcceptsSupportedFormats(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	cases := []struct {
		name string
		data []byte
		ext  string
		mime string
	}{
		{"jpeg", testJPEG(t, 8, 8), "jpg", "image/jpeg"},
		{"gif", testGIF(t, 8, 8), "gif", "image/gif"},
		{"webp", testWebP(t, 8, 8), "webp", "image/webp"},
	}

	for _, tc := range cases {
		result := uploadMedia(t, svc, actor, tc.data, "sample."+tc.ext, "")
		assert.Equal(t, tc.ext, result.Media.Ext, tc.name)
		assert.Equal(t, tc.mime, result.Media.Mime, tc.name)
		assert.Equal(t, 8, result.Media.Width, tc.name)
		assert.FileExists(t, uploadFilePath(result.Media.StorageKey+"/original."+tc.ext), tc.name)

		// JPEG 在 Process 里会再读一次 Orientation（D7 之前的原图可能还带 EXIF）。
		require.NoError(t, svc.Process(result.Media.ID), tc.name)
		updated := findMedia(t, result.Media.ID)
		assert.Equal(t, model.MediaStatusReady, updated.Status, tc.name)
		require.Len(t, mediaVariantFiles(updated), len(mediaVariantTiers), tc.name)
	}
}

func TestMediaService_UploadRejectsInvalidInput(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	// 空文件：没有内容可存。
	_, err := svc.Upload(MediaUploadInput{File: bytes.NewReader(nil), FileName: "empty.png"}, actor)
	appErr, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Contains(t, appErr.Message, "没有收到文件内容")

	// 不是图片：只认 JPEG/PNG/WebP/GIF。
	_, err = svc.Upload(MediaUploadInput{
		File:     strings.NewReader("这是一段纯文本，不是任何图片格式，用来触发格式嗅探的默认分支。"),
		FileName: "a.txt",
	}, actor)
	appErr, ok = errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Contains(t, appErr.Message, "只支持 JPEG、PNG、WebP、GIF")

	// HEIC/AVIF：要给出"请先转码"的明确提示，而不是笼统的不支持。
	heic := append([]byte{0, 0, 0, 0x18}, []byte("ftypheic")...)
	heic = append(heic, make([]byte, 64)...)
	_, err = svc.Upload(MediaUploadInput{File: bytes.NewReader(heic), FileName: "a.heic"}, actor)
	appErr, ok = errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Contains(t, appErr.Message, "HEIC/AVIF")

	// 带 ftyp 但不是 HEIF 家族（例如 MP4）：走普通格式判定，最终仍被拒。
	mp4 := append([]byte{0, 0, 0, 0x18}, []byte("ftypisom")...)
	mp4 = append(mp4, make([]byte, 64)...)
	_, err = svc.Upload(MediaUploadInput{File: bytes.NewReader(mp4), FileName: "a.mp4"}, actor)
	appErr, ok = errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Contains(t, appErr.Message, "只支持")

	// 超过大小上限：写临时文件时就判出来（多读 1 字节即停）。
	global.Config.Upload.MaxBytes = 16
	_, err = svc.Upload(MediaUploadInput{File: bytes.NewReader(testPNG(t, 8, 8)), FileName: "big.png"}, actor)
	appErr, ok = errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorImageTooLarge, appErr.Code)
	global.Config.Upload.MaxBytes = 30 << 20

	// 声明是 PNG 但内容被截断：尺寸都探不出来 → 1003。
	broken := testPNG(t, 8, 8)
	_, err = svc.Upload(MediaUploadInput{File: bytes.NewReader(broken[:20]), FileName: "broken.png"}, actor)
	appErr, ok = errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Contains(t, appErr.Message, "无法解析图片")

	// 以上全部失败的上传都不该留下媒体记录。
	assert.Equal(t, int64(0), countRows(t, "media", ""))

	// maxBytes <= 0 回落到 30MB 默认值：没配上限不等于"什么都超限"。
	global.Config.Upload.MaxBytes = 0
	assert.NotZero(t, uploadMedia(t, svc, actor, testPNG(t, 8, 8), "default-limit.png", "").Media.ID)
	assert.Equal(t, int64(1), countRows(t, "media", ""))
}

// ---------------------------------------------------------------------------
// Process / HandleProcess
// ---------------------------------------------------------------------------

func TestMediaService_ProcessGeneratesVariantsAndReady(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	// 路径一：直接调用 Process。
	media := uploadMedia(t, svc, actor, testPNG(t, 8, 8), "a.png", "").Media
	require.NoError(t, svc.Process(media.ID))

	updated := findMedia(t, media.ID)
	assert.Equal(t, model.MediaStatusReady, updated.Status)
	assert.Empty(t, updated.Error)
	assert.Equal(t, 8, updated.Width)
	assert.Equal(t, 8, updated.Height)
	assert.Equal(t, int64(64), updated.Pixels)
	assert.NotEmpty(t, updated.Blurhash, "转码时应当算出 blurhash 占位图")

	variants := mediaVariantFiles(updated)
	require.Len(t, variants, len(mediaVariantTiers))
	for _, tier := range mediaVariantTiers {
		name := strconv.Itoa(tier)
		assert.Equal(t, name+".webp", variants[name])

		path := uploadFilePath(updated.StorageKey + "/" + name + ".webp")
		info, statErr := os.Stat(path)
		require.NoError(t, statErr, "变体 %s 应当落盘", name)
		assert.Positive(t, info.Size())

		// 8×8 比所有档位都窄，ResizeToWidth 不放大：四档都保持原尺寸。
		config := probeImageFile(t, path)
		assert.Equal(t, 8, config.Width)
		assert.Equal(t, 8, config.Height)
	}

	// Item 现在应当给出四档变体 URL，并优先用 1600 当主图。
	item := svc.Item(updated)
	require.Len(t, item.Variants, len(mediaVariantTiers))
	assert.Equal(t, "/uploads/"+updated.StorageKey+"/1600.webp", item.URL)
	for _, tier := range mediaVariantTiers {
		name := strconv.Itoa(tier)
		assert.Equal(t, "/uploads/"+updated.StorageKey+"/"+name+".webp", item.Variants[name])
	}

	// 路径二：调度器入口 HandleProcess 跑通同一条链路。
	second := uploadMedia(t, svc, actor, testPNG(t, 9, 9), "b.png", "").Media
	job := &model.Job{
		Type:        model.JobTypeMediaProcess,
		Payload:     fmt.Sprintf(`{"mediaId":%d}`, second.ID),
		Attempts:    1,
		MaxAttempts: model.DefaultJobMaxAttempts,
	}
	require.NoError(t, svc.HandleProcess(context.Background(), job))
	assert.Equal(t, model.MediaStatusReady, findMedia(t, second.ID).Status)
}

func TestMediaService_ProcessFailsWhenOriginalMissing(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	media := uploadMedia(t, svc, actor, testPNG(t, 8, 8), "a.png", "").Media

	// 把原图删掉：转码的唯一来源没了。
	require.NoError(t, os.Remove(uploadFilePath(media.StorageKey+"/original.png")))

	// Process 直接报错，且不自己改状态（是否置 failed 由调度器按尝试次数决定）。
	require.Error(t, svc.Process(media.ID))
	assert.Equal(t, model.MediaStatusProcessing, findMedia(t, media.ID).Status)

	// 还有重试机会：HandleProcess 报错但不置 failed。
	job := &model.Job{Payload: fmt.Sprintf(`{"mediaId":%d}`, media.ID), Attempts: 1, MaxAttempts: model.DefaultJobMaxAttempts}
	require.Error(t, svc.HandleProcess(context.Background(), job))
	assert.Equal(t, model.MediaStatusProcessing, findMedia(t, media.ID).Status)

	// 最后一次尝试：置 failed 并把原因写进 error（供管理端排障）。
	job.Attempts = model.DefaultJobMaxAttempts
	require.Error(t, svc.HandleProcess(context.Background(), job))

	updated := findMedia(t, media.ID)
	assert.Equal(t, model.MediaStatusFailed, updated.Status)
	assert.Contains(t, updated.Error, "读取原图失败")
}

func TestMediaService_HandleProcessRejectsBadPayload(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	// 负载不是 JSON、或缺少 mediaId：解析阶段就该失败，不能去查 0 号媒体。
	for _, payload := range []string{"不是 JSON", `{"mediaId":0}`, `{}`} {
		err := svc.HandleProcess(context.Background(), &model.Job{
			Payload: payload, Attempts: model.DefaultJobMaxAttempts, MaxAttempts: model.DefaultJobMaxAttempts,
		})
		require.Error(t, err, "payload=%q", payload)
	}
}

// ---------------------------------------------------------------------------
// Item / List / baseURL
// ---------------------------------------------------------------------------

func TestMediaService_ItemBuildsURLsFromVariants(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	key := mediaKeyFor("item")
	media := insertMediaRecord(t, key, mediaRecordOptions{Variants: fourVariantFiles()})

	item := svc.Item(media)
	assert.Equal(t, media.ID, item.ID)
	assert.Equal(t, media.SHA256, item.SHA256)
	assert.Equal(t, media.Alt, item.Alt)
	assert.Equal(t, media.Blurhash, item.Blurhash)
	assert.Equal(t, media.Width, item.Width)
	assert.Equal(t, media.Height, item.Height)
	assert.Equal(t, media.Pixels, item.Pixels)
	assert.Equal(t, media.CreatedAt, item.CreatedAt)

	// 四档 key 齐全，URL = 前缀 + storage_key + 文件名。
	require.Len(t, item.Variants, len(mediaVariantTiers))
	for _, tier := range mediaVariantTiers {
		name := strconv.Itoa(tier)
		assert.Equal(t, "/uploads/"+key+"/"+name+".webp", item.Variants[name])
	}
	// 主图优先 1600（四档齐全时与 map 迭代顺序无关）。
	assert.Equal(t, "/uploads/"+key+"/1600.webp", item.URL)
	assert.Equal(t, "/uploads/"+key+"/original.png", item.OrigURL)
	assert.False(t, item.OrigArchived)
}

func TestMediaService_BaseURLFollowsSetting(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	settings := SettingService{}

	// 迁移种下的 img_base_url 是空串 → 同源相对路径（nginx 直出）。
	assert.Equal(t, "/uploads", svc.baseURL())

	// 配了前缀：去掉首尾空白与结尾斜杠。
	require.NoError(t, settings.PutString(model.SettingImgBaseURL, "  https://cdn.example.com/img/  "))
	assert.Equal(t, "https://cdn.example.com/img", svc.baseURL())

	// 只有空白等同于没配。
	require.NoError(t, settings.PutString(model.SettingImgBaseURL, "   "))
	assert.Equal(t, "/uploads", svc.baseURL())

	// 脏数据（不是 JSON 字符串）不能让 baseURL 崩掉，退回相对路径。
	putRawSetting(t, model.SettingImgBaseURL, "123")
	assert.Equal(t, "/uploads", svc.baseURL())
}

func TestMediaService_ItemAndListFollowImgBaseURL(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	require.NoError(t, ServiceGroupApp.Setting.PutString(model.SettingImgBaseURL, "https://cdn.example.com/uploads"))

	for i := 0; i < 3; i++ {
		insertMediaRecord(t, mediaKeyFor(fmt.Sprintf("list-%d", i)), mediaRecordOptions{Variants: fourVariantFiles()})
	}

	page1, total, err := svc.List(1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	require.Len(t, page1, 2)

	page2, _, err := svc.List(2, 2)
	require.NoError(t, err)
	require.Len(t, page2, 1)

	// 分页不重不漏。
	ids := map[int64]struct{}{}
	for _, item := range append(append([]MediaItem{}, page1...), page2...) {
		ids[item.ID] = struct{}{}
		assert.Contains(t, item.URL, "https://cdn.example.com/uploads/")
		assert.Contains(t, item.OrigURL, "https://cdn.example.com/uploads/")
	}
	assert.Len(t, ids, 3)
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestMediaService_DeleteRejectsCoverReference(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	media := insertMediaRecord(t, mediaKeyFor("cover"), mediaRecordOptions{Variants: fourVariantFiles()})
	post := insertPost(t, "封面文章", "cover-post", model.PostStatusDraft)
	require.NoError(t, global.DB.Exec(
		`UPDATE posts SET cover_media_id = ? WHERE id = ?`, media.ID, post.ID,
	).Error)

	err := svc.Delete(media.ID, actor)
	appErr, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorMediaInUse, appErr.Code)

	// Details 是引用清单，前端据此提示"还在哪篇文章里用着"。
	refs, ok := appErr.Details.([]model.MediaRef)
	require.True(t, ok, "Details 应当是 []model.MediaRef")
	require.Len(t, refs, 1)
	assert.Equal(t, post.ID, refs[0].PostID)
	assert.Equal(t, "cover", refs[0].Kind)
	assert.Equal(t, "封面文章", refs[0].Title)
	assert.Equal(t, "cover-post", refs[0].Slug)

	// 被拒绝时记录与文件都必须原样保留。
	assert.Equal(t, int64(1), countRows(t, "media", "id = ?", media.ID))
	assert.Equal(t, int64(0), countRows(t, "audit_logs", "action = ?", AuditActionMediaDelete))
}

func TestMediaService_DeleteRejectsContentReference(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	media := insertMediaRecord(t, mediaKeyFor("content"), mediaRecordOptions{Variants: fourVariantFiles()})
	post := insertPost(t, "正文引用", "content-post", model.PostStatusPublished)
	body := "看图：/uploads/" + media.StorageKey + "/1600.webp"
	require.NoError(t, global.DB.Exec(`UPDATE posts SET content_md = ? WHERE id = ?`, body, post.ID).Error)

	err := svc.Delete(media.ID, actor)
	appErr, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorMediaInUse, appErr.Code)

	refs, ok := appErr.Details.([]model.MediaRef)
	require.True(t, ok)
	require.Len(t, refs, 1)
	assert.Equal(t, "content", refs[0].Kind)
	assert.Equal(t, post.ID, refs[0].PostID)

	// 软删除的文章不算引用：回收站里的文章不该永远锁住图片。
	require.NoError(t, global.DB.Exec(`UPDATE posts SET deleted_at = ? WHERE id = ?`, dbtime.Now(), post.ID).Error)
	require.NoError(t, svc.Delete(media.ID, actor))
	assert.Equal(t, int64(0), countRows(t, "media", "id = ?", media.ID))
}

func TestMediaService_DeleteRemovesRecordAndFiles(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	media := uploadMedia(t, svc, actor, testPNG(t, 8, 8), "del.png", "").Media
	require.NoError(t, svc.Process(media.ID))

	dir := uploadFilePath(media.StorageKey)
	require.DirExists(t, dir)

	require.NoError(t, svc.Delete(media.ID, actor))

	assert.Equal(t, int64(0), countRows(t, "media", "id = ?", media.ID))
	assert.NoDirExists(t, dir, "删除媒体应当连整个 storage_key 目录一起清掉")
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionMediaDelete))
}

func TestMediaService_DeleteRejectsProcessingAndMissing(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	// 还在 processing：先让转码跑完再删，否则会删掉正在写的文件。
	processing := uploadMedia(t, svc, actor, testPNG(t, 8, 8), "p.png", "").Media
	err := svc.Delete(processing.ID, actor)
	appErr, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorConflict, appErr.Code)

	// 记录不存在：1002。
	err = svc.Delete(987654, actor)
	appErr, ok = errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorNotFound, appErr.Code)
}

// ---------------------------------------------------------------------------
// RemoveOriginal / Retry
// ---------------------------------------------------------------------------

func TestMediaService_RemoveOriginalKeepsVariantsAndBlocksRetry(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	media := uploadMedia(t, svc, actor, testPNG(t, 8, 8), "orig.png", "").Media
	require.NoError(t, svc.Process(media.ID))

	dir := uploadFilePath(media.StorageKey)
	originalPath := filepath.Join(dir, "original.png")
	require.FileExists(t, originalPath)

	item, err := svc.RemoveOriginal(media.ID, actor)
	require.NoError(t, err)
	assert.True(t, item.OrigArchived)
	assert.Empty(t, item.OrigURL, "原图已删，「查看原图」不该再输出链接")
	assert.NoFileExists(t, originalPath)

	// 四档变体保留，URL 照旧可用。
	require.Len(t, item.Variants, len(mediaVariantTiers))
	for _, tier := range mediaVariantTiers {
		name := strconv.Itoa(tier)
		assert.FileExists(t, filepath.Join(dir, name+".webp"))
		assert.Equal(t, "/uploads/"+media.StorageKey+"/"+name+".webp", item.Variants[name])
	}

	updated := findMedia(t, media.ID)
	require.True(t, updated.OrigArchived)
	require.NotNil(t, updated.OrigArchivedAt)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionMediaOriginalDelete))

	// 原图是重新转码的唯一来源：删掉后 Retry 必须明确报错，而不是静默失败。
	_, err = svc.Retry(media.ID, actor)
	appErr, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorNotFound, appErr.Code)
	assert.Contains(t, appErr.Message, "原图不存在")

	// 重复执行幂等：文件本来就不在，不再写第二条审计。
	again, err := svc.RemoveOriginal(media.ID, actor)
	require.NoError(t, err)
	assert.True(t, again.OrigArchived)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionMediaOriginalDelete))
}

func TestMediaService_RemoveOriginalMissingMedia(t *testing.T) {
	setupEnv(t)

	_, err := MediaService{}.RemoveOriginal(987654, testActor(t))
	appErr, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorNotFound, appErr.Code)
}

func TestMediaService_RetryRegeneratesVariants(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	media := uploadMedia(t, svc, actor, testPNG(t, 8, 8), "retry.png", "").Media
	require.NoError(t, svc.Process(media.ID))

	dir := uploadFilePath(media.StorageKey)
	for _, tier := range mediaVariantTiers {
		require.NoError(t, os.Remove(filepath.Join(dir, strconv.Itoa(tier)+".webp")))
	}

	// Retry 只投递任务，处理是异步的（D8 起）。
	item, err := svc.Retry(media.ID, actor)
	require.NoError(t, err)
	assert.Equal(t, model.MediaStatusProcessing, item.Status)
	assert.Empty(t, item.Error, "重试要清掉上一次的错误信息")

	// 上传 1 条 + Retry 1 条。
	payload := fmt.Sprintf(`{"mediaId":%d}`, media.ID)
	assert.Equal(t, int64(2), countRows(t, "jobs", "type = ? AND payload = ?", model.JobTypeMediaProcess, payload))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionMediaRetry))

	// 测试里没有调度器，手动跑一次转码：变体重新生成、状态回到 ready。
	require.NoError(t, svc.Process(media.ID))
	for _, tier := range mediaVariantTiers {
		assert.FileExists(t, filepath.Join(dir, strconv.Itoa(tier)+".webp"))
	}
	updated := findMedia(t, media.ID)
	assert.Equal(t, model.MediaStatusReady, updated.Status)
	require.Len(t, mediaVariantFiles(updated), len(mediaVariantTiers))

	// processing 中不允许重复 Retry（避免重复投递）。
	require.NoError(t, model.UpdateMediaFields(global.DB, media.ID, map[string]any{
		"status": model.MediaStatusProcessing,
	}))
	_, err = svc.Retry(media.ID, actor)
	appErr, ok := errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorConflict, appErr.Code)

	// 记录不存在：1002。
	_, err = svc.Retry(987654, actor)
	appErr, ok = errs.As(err)
	require.True(t, ok)
	assert.Equal(t, errs.ErrorNotFound, appErr.Code)
}

// ---------------------------------------------------------------------------
// DecorateContent / RefByID
// ---------------------------------------------------------------------------

func TestMediaService_DecorateContentAddsResponsiveAttributes(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	key := mediaKeyFor("decorate")
	insertMediaRecord(t, key, mediaRecordOptions{Width: 1200, Height: 800, Variants: fourVariantFiles()})

	content := `<p>看图</p><img src="/uploads/` + key + `/1600.webp" alt="图">`
	got := svc.DecorateContent(content)

	// srcset 用四档变体，按档位升序拼，sizes 用正文宽度。
	parts := make([]string, 0, len(mediaVariantTiers))
	for _, tier := range mediaVariantTiers {
		parts = append(parts, fmt.Sprintf("/uploads/%s/%d.webp %dw", key, tier, tier))
	}
	assert.Contains(t, got, `srcset="`+strings.Join(parts, ", ")+`"`)
	assert.Contains(t, got, `sizes="`+mediaContentSizes+`"`)

	// 尺寸防抖动 + 懒加载。
	assert.Contains(t, got, `width="1200"`)
	assert.Contains(t, got, `height="800"`)
	assert.Contains(t, got, `loading="lazy"`)
	assert.Contains(t, got, `decoding="async"`)

	// 原图还在本地 → 外面包一层「查看原图」链接。
	assert.Contains(t, got,
		`<a class="media-orig" href="/uploads/`+key+`/original.png" target="_blank" rel="noopener noreferrer">`)
}

func TestMediaService_DecorateContentKeepsExistingAttrsAndLeavesForeignImages(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	key := mediaKeyFor("existing")
	insertMediaRecord(t, key, mediaRecordOptions{Width: 1200, Height: 800, Variants: fourVariantFiles()})

	content := `<img src="/uploads/` + key + `/1600.webp" width="300" loading="eager">` +
		`<img src="https://example.com/x.png" alt="外链">`
	got := svc.DecorateContent(content)

	// 作者显式写过的 width / loading 一律不覆盖。
	assert.Contains(t, got, `width="300"`)
	assert.NotContains(t, got, `width="1200"`)
	assert.Contains(t, got, `loading="eager"`)
	assert.NotContains(t, got, `loading="lazy"`)

	// 外链图片原样保留（既不补属性，也不包原图链接）。
	assert.Contains(t, got, `<img src="https://example.com/x.png" alt="外链">`)
}

func TestMediaService_DecorateContentFollowsImgBaseURL(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	key := mediaKeyFor("cdn")
	insertMediaRecord(t, key, mediaRecordOptions{Variants: fourVariantFiles()})

	require.NoError(t, ServiceGroupApp.Setting.PutString(model.SettingImgBaseURL, "https://cdn.example.com/uploads"))

	// 这是修过的缺陷：匹配前缀必须跟着 img_base_url 走，不能硬编码 /uploads。
	cdn := `<img src="https://cdn.example.com/uploads/` + key + `/1600.webp">`
	got := svc.DecorateContent(cdn)
	assert.Contains(t, got, "srcset=", "配了跨源前缀时也要能补上 srcset")
	assert.Contains(t, got, `sizes="`+mediaContentSizes+`"`)
	assert.Contains(t, got, `href="https://cdn.example.com/uploads/`+key+`/original.png"`)

	// 前缀换掉之后，同源相对路径不再被当成本站图片。
	sameOrigin := `<img src="/uploads/` + key + `/1600.webp">`
	assert.Equal(t, sameOrigin, svc.DecorateContent(sameOrigin))
}

func TestMediaService_DecorateContentNoopCases(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	// 空正文 / 没有 img：原样返回。
	assert.Empty(t, svc.DecorateContent(""))
	plain := "<p>没有图片</p>"
	assert.Equal(t, plain, svc.DecorateContent(plain))

	// 库里查不到这个 storage_key（不是本站上传，或记录已删）：原样返回。
	unknown := `<img src="/uploads/` + mediaKeyFor("unknown") + `/1600.webp">`
	assert.Equal(t, unknown, svc.DecorateContent(unknown))

	// 只有一个变体：不足两档不输出 srcset，但尺寸与懒加载照加。
	key := mediaKeyFor("single")
	insertMediaRecord(t, key, mediaRecordOptions{Width: 640, Height: 480, Variants: map[string]string{"480": "480.webp"}})
	single := `<img src="/uploads/` + key + `/480.webp">`
	got := svc.DecorateContent(single)
	assert.NotContains(t, got, "srcset=")
	assert.Contains(t, got, `width="640"`)
	assert.Contains(t, got, `height="480"`)
	assert.Contains(t, got, `loading="lazy"`)
}

func TestMediaService_DecorateContentHandlesPartialMatches(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	known := mediaKeyFor("partial-known")
	insertMediaRecord(t, known, mediaRecordOptions{Variants: fourVariantFiles()})
	unknown := mediaKeyFor("partial-unknown")

	// 同一张图出现两次：storage_key 去重后只查一次库，两个标签都要被装饰。
	// 中间夹一张"匹配前缀但库里没有"的图与一个没有 src 的 img：两者都原样保留。
	content := `<img src="/uploads/` + known + `/1600.webp">` +
		`<img alt="没有 src">` +
		`<img src="/uploads/` + unknown + `/1600.webp">` +
		`<img src="/uploads/` + known + `/960.webp">`
	got := svc.DecorateContent(content)

	assert.Equal(t, 2, strings.Count(got, "srcset="), "两张已知图片都要补 srcset")
	assert.Equal(t, 2, strings.Count(got, `class="media-orig"`))
	assert.Contains(t, got, `<img alt="没有 src">`, "没有 src 的标签原样保留")
	assert.Contains(t, got, `<img src="/uploads/`+unknown+`/1600.webp">`, "库里查不到的图片原样保留")
}

func TestMediaService_DecorateContentWithoutOriginalLink(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	// 原图已被管理端删除（orig_archived）：只补响应式属性，不再包「查看原图」链接。
	key := mediaKeyFor("archived-decorate")
	insertMediaRecord(t, key, mediaRecordOptions{Variants: fourVariantFiles(), OrigArchived: true})

	got := svc.DecorateContent(`<img src="/uploads/` + key + `/1600.webp">`)
	assert.Contains(t, got, "srcset=")
	assert.Contains(t, got, `width="1200"`)
	assert.NotContains(t, got, "media-orig")
}

func TestMediaService_InvalidStorageKeyIsRejectedByPathResolver(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	// storage_key 越出 upload.dir：storage.Local 必须拒绝解析（不能读写根目录外的文件）。
	media := insertMediaRecord(t, "../evil", mediaRecordOptions{})

	_, err := svc.RemoveOriginal(media.ID, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "生成原图路径失败")

	_, err = svc.Retry(media.ID, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "生成原图路径失败")

	// Process 同样在解析原图路径时就拒绝越界 key。
	require.Error(t, svc.Process(media.ID))

	// 记录本身仍然删得掉，只是清不掉目录（路径非法时跳过文件清理）。
	require.NoError(t, svc.Delete(media.ID, actor))
	assert.Equal(t, int64(0), countRows(t, "media", "id = ?", media.ID))
}

func TestMediaService_ItemWithout1600VariantPicksArbitraryTier(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	// 疑似缺陷：四档里没有 1600（例如手工改过 variants_json）时，主图取 map 迭代到的第一条，
	// 而 Go 的 map 迭代顺序是随机的 —— 同一个媒体在不同请求里可能给出不同的主图 URL。
	// 这里只能断言"是其中一条"，无法断言稳定取值。
	key := mediaKeyFor("no-1600")
	media := insertMediaRecord(t, key, mediaRecordOptions{
		Variants: map[string]string{"480": "480.webp", "960": "960.webp"},
	})

	item := svc.Item(media)
	assert.Contains(t, []string{
		"/uploads/" + key + "/480.webp",
		"/uploads/" + key + "/960.webp",
	}, item.URL)
}

func TestMediaService_ProcessPartialFailureLeavesStrayVariant(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	media := uploadMedia(t, svc, actor, testPNG(t, 8, 8), "partial.png", "").Media
	dir := uploadFilePath(media.StorageKey)

	// 让第二档变体的目标路径被一个目录占住：第一档写完，第二档 rename 失败。
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "960.webp"), 0o750))

	err := svc.Process(media.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "生成 960 变体失败")

	// 失败后状态与 variants_json 不变（调度器据此置 failed），但第一档文件已留在磁盘上。
	updated := findMedia(t, media.ID)
	assert.Equal(t, model.MediaStatusProcessing, updated.Status)
	assert.Equal(t, "{}", updated.VariantsJSON)

	stray := filepath.Join(dir, "480.webp")
	info, statErr := os.Stat(stray)
	require.NoError(t, statErr)
	assert.Positive(t, info.Size())

	// 疑似缺陷：这份没被 variants_json 记录的残留文件，因为所在 key 仍有媒体记录，
	// 对账时会当成"在用文件"（计入 VariantBytes 而不是孤儿），清理接口永远收不走。
	overview, err := svc.StorageOverview()
	require.NoError(t, err)
	assert.Zero(t, overview.OrphanCount)
	assert.Equal(t, info.Size(), overview.VariantBytes)
}

func TestMediaService_RefByID(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	key := mediaKeyFor("ref")
	media := insertMediaRecord(t, key, mediaRecordOptions{Variants: fourVariantFiles()})

	thumb, err := svc.RefByID(media.ID)
	require.NoError(t, err)
	require.NotNil(t, thumb)
	assert.Equal(t, media.ID, thumb.ID)
	assert.Equal(t, "/uploads/"+key+"/1600.webp", thumb.URL)
	assert.Equal(t, media.Alt, thumb.Alt)
	assert.Equal(t, media.Width, thumb.Width)
	assert.Equal(t, media.Height, thumb.Height)

	// 记录不存在时返回 (nil, nil)：封面上挂着已删的 id 不该让详情接口报错。
	thumb, err = svc.RefByID(987654)
	require.NoError(t, err)
	assert.Nil(t, thumb)
}

// ---------------------------------------------------------------------------
// storage.go：对账、截断、清理
// ---------------------------------------------------------------------------

func TestMediaService_StorageOverviewReconcilesDiskAndDB(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	liveOriginal := []byte("LIVE-ORIGINAL")
	liveVariant := []byte("LIVE-VARIANT")
	archivedVariant := []byte("ARCH-VARIANT")
	attachmentBytes := []byte("LIVE-ATTACH")
	orphanOriginal := []byte("ORPHAN-ORIG")
	orphanAttachment := []byte("ORPHAN-ATT")

	// 在用媒体：记录 + 原图 + 四档变体都在。
	liveKey := mediaKeyFor("live")
	insertMediaRecord(t, liveKey, mediaRecordOptions{})
	writeTestFile(t, uploadFilePath(liveKey+"/original.png"), liveOriginal)
	for _, tier := range mediaVariantTiers {
		writeTestFile(t, uploadFilePath(fmt.Sprintf("%s/%d.webp", liveKey, tier)), liveVariant)
	}

	// 已删原图的媒体：只剩四档变体（不参与"缺失"统计）。
	archivedKey := mediaKeyFor("archived")
	insertMediaRecord(t, archivedKey, mediaRecordOptions{OrigArchived: true})
	for _, tier := range mediaVariantTiers {
		writeTestFile(t, uploadFilePath(fmt.Sprintf("%s/%d.webp", archivedKey, tier)), archivedVariant)
	}

	// 缺失媒体：库里有记录，原图与四档变体磁盘上都没有。
	missingKey := mediaKeyFor("missing")
	missing := insertMediaRecord(t, missingKey, mediaRecordOptions{})

	// 孤儿：磁盘上有文件，库里没有任何记录。
	orphanKey := mediaKeyFor("orphan")
	writeTestFile(t, uploadFilePath(orphanKey+"/original.png"), orphanOriginal)

	// 附件：一条在用、一条孤儿、一条"库里有记录但文件丢了"。
	insertAttachmentRecord(t, "dd/live.zip")
	writeTestFile(t, fileFilePath("dd/live.zip"), attachmentBytes)
	writeTestFile(t, fileFilePath("ee/orphan.zip"), orphanAttachment)
	missingAttachment := insertAttachmentRecord(t, "ff/missing.zip")

	// 上传中转目录永远不算孤儿。
	writeTestFile(t, uploadFilePath(".tmp/half-upload"), []byte("junk"))
	overview, err := svc.StorageOverview()
	require.NoError(t, err)

	assert.Equal(t, global.Config.Upload.Dir, overview.UploadDir)
	assert.Equal(t, global.Config.File.Dir, overview.FileDir)
	assert.Equal(t, int64(3), overview.MediaCount)
	assert.Equal(t, int64(1), overview.OriginalsRemoved)
	assert.Equal(t, int64(2), overview.AttachmentCount)
	assert.Equal(t, int64(2), overview.LinkedAttachment)

	// 占用统计：原图 / 变体 / 附件分开算，总数是三者之和。
	assert.Equal(t, int64(len(liveOriginal)), overview.OriginalBytes)
	assert.Equal(t, int64(4*(len(liveVariant)+len(archivedVariant))), overview.VariantBytes)
	assert.Equal(t, int64(len(attachmentBytes)), overview.AttachmentBytes)
	assert.Equal(t, overview.OriginalBytes+overview.VariantBytes+overview.AttachmentBytes, overview.TotalBytes)

	// 孤儿：一个图片孤儿 + 一个附件孤儿，按路径排序。
	assert.Equal(t, int64(2), overview.OrphanCount)
	assert.Equal(t, int64(len(orphanOriginal)+len(orphanAttachment)), overview.OrphanBytes)
	require.Len(t, overview.Orphans, 2)
	assert.Equal(t, orphanKey+"/original.png", overview.Orphans[0].Path)
	assert.Equal(t, "original", overview.Orphans[0].Kind)
	assert.Equal(t, int64(len(orphanOriginal)), overview.Orphans[0].Bytes)
	assert.Equal(t, "ee/orphan.zip", overview.Orphans[1].Path)
	assert.Equal(t, "attachment", overview.Orphans[1].Kind)

	// 缺失：缺失媒体（原图 1 + 四档变体 4）+ 丢文件的附件 1 条。
	assert.Equal(t, int64(6), overview.MissingCount)
	require.Len(t, overview.Missing, 6)
	kinds := map[string]int{}
	attachmentMissing := 0
	for _, item := range overview.Missing {
		kinds[item.Kind]++
		if item.Kind == "attachment" {
			attachmentMissing++
			assert.Equal(t, missingAttachment.ID, item.AttachmentID)
			assert.Equal(t, "ff/missing.zip", item.StorageKey)
			continue
		}
		assert.Equal(t, missing.ID, item.MediaID)
		// 原图条目只记 storage_key，变体条目带上文件名（便于人工定位）。
		assert.True(t, strings.HasPrefix(item.StorageKey, missingKey), item.StorageKey)
	}
	assert.Equal(t, 1, kinds["original"])
	assert.Equal(t, 4, kinds["variant"])
	assert.Equal(t, 1, attachmentMissing)
}

func TestMediaService_ScanStorageOrphanLimit(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	// 刻意超过概览上限：计数是全量，清单才截断。
	total := storageOrphanListMax + 5
	for i := 0; i < total; i++ {
		writeTestFile(t, uploadFilePath(fmt.Sprintf("zz/o%03d/original.png", i)), []byte("x"))
	}

	overview, err := svc.StorageOverview()
	require.NoError(t, err)
	assert.Equal(t, int64(total), overview.OrphanCount)
	assert.Len(t, overview.Orphans, storageOrphanListMax, "概览清单按 storageOrphanListMax 截断")

	// orphanLimit <= 0 表示不截断（清理依赖这个全量清单）。
	full, err := svc.scanStorage(0)
	require.NoError(t, err)
	assert.Equal(t, int64(total), full.OrphanCount)
	assert.Len(t, full.Orphans, total)

	// 空 storage_key 的记录不参与对账：既不算"在用"，也不算"缺失"。
	insertMediaRecord(t, "", mediaRecordOptions{})
	insertAttachmentRecord(t, "")
	skipped, err := svc.scanStorage(0)
	require.NoError(t, err)
	assert.Equal(t, int64(total), skipped.OrphanCount, "空 key 的记录不该影响孤儿统计")
	assert.Zero(t, skipped.MissingCount, "空 key 的记录不该被算成缺失")
	assert.Zero(t, skipped.LinkedAttachment, "空 key 的附件不算已链接")
}

func TestMediaService_CleanupOrphansRemovesAllOrphans(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}
	actor := testActor(t)

	// 在用文件必须留下。
	liveKey := mediaKeyFor("cleanup-live")
	live := insertMediaRecord(t, liveKey, mediaRecordOptions{})
	writeTestFile(t, uploadFilePath(liveKey+"/original.png"), []byte("live"))

	// 孤儿数量刻意超过概览上限 200：清理必须是全量，否则永远清不完（修过的缺陷）。
	total := storageOrphanListMax + 5
	for i := 0; i < total; i++ {
		writeTestFile(t, uploadFilePath(fmt.Sprintf("zz/o%03d/original.png", i)), []byte("x"))
	}
	writeTestFile(t, fileFilePath("ee/orphan.zip"), []byte("x"))

	result, err := svc.CleanupOrphans(actor)
	require.NoError(t, err)
	assert.Equal(t, total+1, result.RemovedFiles, "清理要走全量扫描，不能被 200 条的清单上限卡住")
	assert.Equal(t, int64(total+1), result.RemovedBytes)
	assert.Zero(t, result.FailedFiles)

	// 孤儿文件与被清空的目录都被收掉。
	assert.NoDirExists(t, uploadFilePath("zz"))
	assert.NoDirExists(t, fileFilePath("ee"))

	// 在用文件不受影响。
	assert.FileExists(t, uploadFilePath(liveKey+"/original.png"))
	assert.Equal(t, int64(1), countRows(t, "media", "id = ?", live.ID))

	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionStorageCleanup))
}

func TestMediaService_StorageOverviewReportsImageRootFailure(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	// 路径里带 NUL 字节：Lstat 返回的是 EINVAL 而不是 ErrNotExist，扫描必须如实报错，
	// 不能把"根本读不到的目录"当成空目录（否则会误报 0 占用、0 孤儿）。
	global.Config.Upload.Dir = "\x00bad"

	_, err := svc.StorageOverview()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "扫描图片目录失败")

	// 清理同样先对账：扫不到目录时必须报错退出，绝不能当成"没有孤儿"而误报成功。
	_, err = svc.CleanupOrphans(testActor(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "扫描图片目录失败")
}

func TestMediaService_StorageOverviewReportsAttachmentRootFailure(t *testing.T) {
	setupEnv(t)
	svc := MediaService{}

	global.Config.File.Dir = "\x00bad"

	_, err := svc.StorageOverview()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "扫描附件目录失败")
}
