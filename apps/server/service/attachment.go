package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/storage"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
)

// TrackAttachmentClick 给附件下载计数 +1（公开端 /go 用）。
//
// 计数失败只记日志：访客要的是文件，统计丢了不该让跳转失败。
func (s ContentService) TrackAttachmentClick(id int64) {
	if err := model.IncrementAttachmentClicks(global.DB, id); err != nil {
		global.Log.Warn("累加附件下载数失败", "attachment_id", id, "err", err)
	}
}

// attachmentExtBlocked 是不允许上传的扩展名（黑名单，agent.md §10）。
//
// 用黑名单而不是白名单：附件是站长的课程资料 / 数据集，类型无法穷举；而文件既不会被
// 执行、也不会被直出（一律 Content-Disposition: attachment + octet-stream），
// 所以只需挡住"能在服务器上跑、能被浏览器当页面渲染、或本身就是可执行程序"的那几类。
var attachmentExtBlocked = map[string]struct{}{
	// 服务端脚本（nginx / php-fpm 万一误配就可能被执行）
	"php": {}, "php2": {}, "php3": {}, "php4": {}, "php5": {}, "php6": {}, "php7": {}, "php8": {},
	"phps": {}, "phtml": {}, "pht": {}, "phar": {}, "inc": {},
	"asp": {}, "aspx": {}, "ashx": {}, "asmx": {}, "ascx": {}, "axd": {}, "asa": {}, "cer": {},
	"jsp": {}, "jspx": {}, "jspf": {}, "jsw": {}, "jsv": {},
	"cgi": {}, "pl": {}, "pm": {}, "py": {}, "pyc": {}, "pyo": {}, "rb": {}, "erb": {}, "lua": {},
	"sh": {}, "bash": {}, "zsh": {}, "ksh": {}, "csh": {}, "fish": {},
	"htaccess": {}, "htpasswd": {},
	// 浏览器会当页面渲染的（防 XSS / 钓鱼页）
	"html": {}, "htm": {}, "xhtml": {}, "shtml": {}, "shtm": {}, "hta": {}, "svg": {}, "svgz": {},
	"xht": {}, "xhtm": {}, "mhtml": {}, "mht": {}, "xml": {},
	// Windows 可执行与脚本（避免站点变成恶意软件分发点）
	"exe": {}, "msi": {}, "msp": {}, "mst": {}, "com": {}, "scr": {}, "pif": {}, "cpl": {},
	"bat": {}, "cmd": {}, "lnk": {}, "inf": {}, "reg": {}, "sys": {}, "drv": {}, "ocx": {},
	"vbs": {}, "vbe": {}, "js": {}, "jse": {}, "mjs": {}, "cjs": {}, "ws": {}, "wsf": {}, "wsh": {},
	"ps1": {}, "psm1": {}, "psd1": {}, "ps1xml": {}, "ps2": {}, "ps2xml": {}, "psc1": {}, "psc2": {},
	"jar": {}, "class": {}, "war": {}, "ear": {},
	// 装机镜像 / 安装包：体积大且常被用来传毒
	"apk": {}, "xap": {}, "dmg": {}, "iso": {}, "img": {}, "deb": {}, "rpm": {}, "swf": {},
}

// attachmentNameBlocked 是不允许上传的固定文件名（web 服务器与系统配置文件）。
var attachmentNameBlocked = map[string]struct{}{
	".htaccess": {}, ".htpasswd": {}, ".user.ini": {}, "web.config": {},
	"php.ini": {}, "crossdomain.xml": {}, "desktop.ini": {}, "thumbs.db": {},
}

// AttachmentUploadInput 是一次附件上传的入参；File 由 api 层从 multipart part 直接传入。
type AttachmentUploadInput struct {
	File     io.Reader
	FileName string
	PostID   int64
	Title    string
	Version  string
	Sort     *int
}

// UploadAttachment 校验命名 → 流式落盘（按 sha256 去重）→ 建附件记录（§10）。
//
// 物理文件是 {file.dir}/{sha[:2]}/{sha256}{ext}，storage_key 存整段相对路径；
// 同一份文件重复上传只占一份磁盘，多条附件记录共用同一个 storage_key。
func (s ContentService) UploadAttachment(in AttachmentUploadInput, actor PostActor) (*model.PostAttachment, error) {
	name, ext, err := checkAttachmentName(in.FileName)
	if err != nil {
		return nil, err
	}
	if in.PostID <= 0 {
		return nil, contentFieldError([]request.FieldError{{Field: "postId", Message: "必须是正整数"}})
	}
	if err := ensurePostExists(in.PostID); err != nil {
		return nil, err
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = strings.TrimSuffix(name, filepath.Ext(name))
	}
	if n := utf8.RuneCountInString(title); n == 0 || n > attachmentTitleMaxLen {
		return nil, contentFieldError([]request.FieldError{
			{Field: "title", Message: "标题长度必须在 1–200 个字符之间"},
		})
	}

	sort := 0
	if in.Sort != nil {
		sort = *in.Sort
	}
	if sort < 0 || sort > attachmentSortMax {
		return nil, contentFieldError([]request.FieldError{
			{Field: "sort", Message: "必须在 0–99999 之间"},
		})
	}

	root := global.Config.File.Dir
	maxBytes := global.Config.File.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 20 << 20
	}

	tempDir := filepath.Join(root, ".tmp")
	if err := os.MkdirAll(tempDir, 0o750); err != nil {
		return nil, fmt.Errorf("创建附件临时目录失败: %w", err)
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
		return nil, fmt.Errorf("接收附件失败: %w", err)
	}
	if written > maxBytes {
		return nil, errs.New(errs.ErrorFileTooLarge,
			fmt.Sprintf("附件超过 %d MB 上限", maxBytes>>20))
	}
	if written == 0 {
		return nil, errs.New(errs.ErrorValidation, "附件内容为空")
	}

	digest := hex.EncodeToString(hasher.Sum(nil))
	key := digest[:2] + "/" + digest + ext

	store := storage.Local{Root: root}
	finalPath, err := store.Path(key)
	if err != nil {
		return nil, fmt.Errorf("生成附件路径失败: %w", err)
	}
	if _, statErr := os.Stat(finalPath); errors.Is(statErr, os.ErrNotExist) {
		if mkErr := os.MkdirAll(filepath.Dir(finalPath), 0o750); mkErr != nil {
			return nil, fmt.Errorf("创建附件目录失败: %w", mkErr)
		}
		if renameErr := os.Rename(tempPath, finalPath); renameErr != nil {
			return nil, fmt.Errorf("保存附件失败: %w", renameErr)
		}
	}

	mime := sniffAttachmentMime(finalPath)
	now := dbtime.Now()
	item := &model.PostAttachment{
		PostID:     in.PostID,
		Title:      title,
		FileName:   name,
		Bytes:      written,
		SHA256:     digest,
		Version:    truncateRunes(strings.TrimSpace(in.Version), attachmentVersionMaxLen),
		Sort:       sort,
		StorageKey: key,
		Mime:       mime,
		CreatedAt:  now,
	}

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateAttachment(tx, item); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionAttachmentUpload,
			TargetType: "attachment",
			TargetID:   fmt.Sprintf("%d", item.ID),
			Meta:       map[string]any{"postId": item.PostID, "fileName": item.FileName, "bytes": item.Bytes},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, fmt.Errorf("创建附件失败: %w", err)
	}
	return item, nil
}

// AttachmentPath 返回附件在磁盘上的绝对路径；外链附件（storage_key 为空）返回空串。
func AttachmentPath(item *model.PostAttachment) (string, error) {
	if item == nil || item.StorageKey == "" {
		return "", nil
	}
	return storage.Local{Root: global.Config.File.Dir}.Path(item.StorageKey)
}

// PurgeUnreferencedAttachmentFiles 删除已无任何附件记录引用的文件（删附件 / 物理删文章后用）。
//
// 同一份文件可能被多篇文章共用，所以必须先数引用再删。返回真正删掉的文件数。
func (s ContentService) PurgeUnreferencedAttachmentFiles(keys []string) int {
	removed := 0
	for _, key := range keys {
		if key == "" {
			continue
		}
		count, err := model.CountAttachmentsByStorageKey(global.DB, key, 0)
		if err != nil {
			global.Log.Warn("统计附件引用失败", "storage_key", key, "err", err)
			continue
		}
		if count > 0 {
			continue
		}
		path, err := storage.Local{Root: global.Config.File.Dir}.Path(key)
		if err != nil {
			global.Log.Warn("跳过非法附件路径", "storage_key", key, "err", err)
			continue
		}
		switch err := os.Remove(path); {
		case err == nil:
			removed++
		case errors.Is(err, os.ErrNotExist):
		default:
			global.Log.Warn("删除附件文件失败", "storage_key", key, "err", err)
		}
	}
	return removed
}

// checkAttachmentName 清洗并校验上传文件名，返回可用文件名与规范化扩展名（含点）。
func checkAttachmentName(raw string) (name, ext string, err error) {
	name = sanitizeAttachmentName(raw)
	if name == "" {
		return "", "", errs.New(errs.ErrorValidation, "缺少文件名")
	}
	if strings.HasPrefix(name, ".") {
		return "", "", errs.New(errs.ErrorFileRejected, "不允许上传隐藏文件")
	}
	if utf8.RuneCountInString(name) > attachmentFileNameMaxLen {
		current := filepath.Ext(name)
		name = truncateRunes(strings.TrimSuffix(name, current),
			attachmentFileNameMaxLen-utf8.RuneCountInString(current)) + current
	}
	if _, blocked := attachmentNameBlocked[strings.ToLower(name)]; blocked {
		return "", "", errs.New(errs.ErrorFileRejected, "该文件名不允许作为附件")
	}
	if current := filepath.Ext(name); current != "" {
		if _, blocked := attachmentExtBlocked[strings.ToLower(strings.TrimPrefix(current, "."))]; blocked {
			return "", "", errs.New(errs.ErrorFileRejected, "出于安全考虑，不允许上传该类型的文件")
		}
		ext = strings.ToLower(current)
	}
	return name, ext, nil
}

// sanitizeAttachmentName 只保留基础文件名并去掉控制字符，避免路径穿越与超长名字。
func sanitizeAttachmentName(raw string) string {
	base := strings.TrimSpace(raw)
	base = strings.ReplaceAll(base, "\\", "/")
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	base = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, base)
	if base == "." || base == ".." {
		return ""
	}
	return base
}

// sniffAttachmentMime 只读文件头猜 MIME，仅作展示与排障用；下载时一律发 octet-stream。
func sniffAttachmentMime(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return ""
	}
	detected := http.DetectContentType(head[:n])
	if detected == "application/octet-stream" {
		return ""
	}
	return truncateRunes(detected, 100)
}
