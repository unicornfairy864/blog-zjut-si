package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
)

// 存储管理（§10）：原图与附件不再自动清理，空间回收改由管理端显式操作。
const (
	// storageOrphanListMax 是概览里最多列出的孤儿/缺失文件条数（总数仍照实统计）。
	storageOrphanListMax = 200
	// storageMissingListMax 是概览里最多列出的缺失文件条数。
	storageMissingListMax = 100
)

// StorageFile 是一条磁盘文件（孤儿清单用）。
type StorageFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Kind  string `json:"kind"`
}

// StorageMissing 是一条"库里有记录、磁盘上没有文件"的对账结果。
type StorageMissing struct {
	Kind         string `json:"kind"`
	StorageKey   string `json:"storageKey"`
	MediaID      int64  `json:"mediaId,omitempty"`
	AttachmentID int64  `json:"attachmentId,omitempty"`
}

// StorageOverview 是 GET /api/admin/storage 的输出（§5）。
type StorageOverview struct {
	UploadDir        string           `json:"uploadDir"`
	FileDir          string           `json:"fileDir"`
	MediaCount       int64            `json:"mediaCount"`
	OriginalBytes    int64            `json:"originalBytes"`
	VariantBytes     int64            `json:"variantBytes"`
	OriginalsRemoved int64            `json:"originalsRemoved"`
	AttachmentCount  int64            `json:"attachmentCount"`
	AttachmentBytes  int64            `json:"attachmentBytes"`
	LinkedAttachment int64            `json:"linkedAttachmentCount"`
	TotalBytes       int64            `json:"totalBytes"`
	OrphanCount      int64            `json:"orphanCount"`
	OrphanBytes      int64            `json:"orphanBytes"`
	MissingCount     int64            `json:"missingCount"`
	Orphans          []StorageFile    `json:"orphans"`
	Missing          []StorageMissing `json:"missing"`
}

// StorageCleanupResult 是孤儿清理的结果。
type StorageCleanupResult struct {
	RemovedFiles int   `json:"removedFiles"`
	RemovedBytes int64 `json:"removedBytes"`
	FailedFiles  int   `json:"failedFiles"`
}

// StorageOverview 对账磁盘与数据库：统计占用、列出孤儿文件与缺失文件（§10）。
//
// 判定规则：文件所在目录名（图片）或相对路径（附件）就是 storage_key，
// 库里有对应记录即为"在用"，否则是孤儿；库里记录了但文件不在即为"缺失"。
func (s MediaService) StorageOverview() (StorageOverview, error) {
	overview := StorageOverview{
		UploadDir: global.Config.Upload.Dir,
		FileDir:   global.Config.File.Dir,
		Orphans:   make([]StorageFile, 0),
		Missing:   make([]StorageMissing, 0),
	}

	mediaList, err := model.ListAllMedia(global.DB)
	if err != nil {
		return StorageOverview{}, fmt.Errorf("查询媒体失败: %w", err)
	}
	attachments, err := model.ListAllAttachments(global.DB)
	if err != nil {
		return StorageOverview{}, fmt.Errorf("查询附件失败: %w", err)
	}

	mediaByKey := make(map[string]*model.Media, len(mediaList))
	for i := range mediaList {
		if mediaList[i].StorageKey != "" {
			mediaByKey[mediaList[i].StorageKey] = &mediaList[i]
		}
		if mediaList[i].OrigArchived {
			overview.OriginalsRemoved++
		}
	}
	overview.MediaCount = int64(len(mediaList))

	attachmentKeys := make(map[string]int64, len(attachments))
	for i := range attachments {
		if attachments[i].StorageKey != "" {
			attachmentKeys[attachments[i].StorageKey] = attachments[i].ID
		}
	}
	overview.AttachmentCount = int64(len(attachments))

	// 图片目录：{upload.dir}/{sha[:2]}/{sha256}/{original.ext|档位.webp}
	uploadRoot := global.Config.Upload.Dir
	if uploadRoot != "" {
		walkErr := filepath.WalkDir(uploadRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == ".tmp" {
					return fs.SkipDir
				}
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil
			}
			relative, relErr := filepath.Rel(uploadRoot, path)
			if relErr != nil {
				return nil
			}
			key := filepath.ToSlash(filepath.Dir(relative))
			kind := "variant"
			if strings.HasPrefix(entry.Name(), "original.") {
				kind = "original"
			}
			if _, ok := mediaByKey[key]; !ok {
				overview.OrphanCount++
				overview.OrphanBytes += info.Size()
				if len(overview.Orphans) < storageOrphanListMax {
					overview.Orphans = append(overview.Orphans, StorageFile{
						Path: filepath.ToSlash(relative), Bytes: info.Size(), Kind: kind,
					})
				}
				return nil
			}
			if kind == "original" {
				overview.OriginalBytes += info.Size()
			} else {
				overview.VariantBytes += info.Size()
			}
			return nil
		})
		if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
			return StorageOverview{}, fmt.Errorf("扫描图片目录失败: %w", walkErr)
		}
	}

	// 附件目录：{file.dir}/{sha[:2]}/{sha256}{ext}
	fileRoot := global.Config.File.Dir
	if fileRoot != "" {
		walkErr := filepath.WalkDir(fileRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == ".tmp" {
					return fs.SkipDir
				}
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil
			}
			relative, relErr := filepath.Rel(fileRoot, path)
			if relErr != nil {
				return nil
			}
			key := filepath.ToSlash(relative)
			if _, ok := attachmentKeys[key]; !ok {
				overview.OrphanCount++
				overview.OrphanBytes += info.Size()
				if len(overview.Orphans) < storageOrphanListMax {
					overview.Orphans = append(overview.Orphans, StorageFile{
						Path: key, Bytes: info.Size(), Kind: "attachment",
					})
				}
				return nil
			}
			overview.AttachmentBytes += info.Size()
			return nil
		})
		if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
			return StorageOverview{}, fmt.Errorf("扫描附件目录失败: %w", walkErr)
		}
	}

	overview.LinkedAttachment = int64(len(attachmentKeys))
	overview.TotalBytes = overview.OriginalBytes + overview.VariantBytes + overview.AttachmentBytes

	// 缺失文件：库里记着、磁盘上找不到（外链附件与已删原图的图片不算）。
	for i := range mediaList {
		media := &mediaList[i]
		if media.StorageKey == "" {
			continue
		}
		if !media.OrigArchived {
			if _, statErr := os.Stat(filepath.Join(uploadRoot, filepath.FromSlash(media.StorageKey),
				"original."+media.Ext)); errors.Is(statErr, os.ErrNotExist) {
				overview.addMissing(StorageMissing{
					Kind: "original", StorageKey: media.StorageKey, MediaID: media.ID,
				})
			}
		}
		for _, tier := range mediaVariantTiers {
			name := fmt.Sprintf("%d.webp", tier)
			if _, statErr := os.Stat(filepath.Join(uploadRoot, filepath.FromSlash(media.StorageKey), name)); errors.Is(statErr, os.ErrNotExist) {
				overview.addMissing(StorageMissing{
					Kind: "variant", StorageKey: media.StorageKey + "/" + name, MediaID: media.ID,
				})
			}
		}
	}
	for i := range attachments {
		item := &attachments[i]
		if item.StorageKey == "" {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(fileRoot, filepath.FromSlash(item.StorageKey))); errors.Is(statErr, os.ErrNotExist) {
			overview.addMissing(StorageMissing{
				Kind: "attachment", StorageKey: item.StorageKey, AttachmentID: item.ID,
			})
		}
	}

	sort.Slice(overview.Orphans, func(i, j int) bool {
		return overview.Orphans[i].Path < overview.Orphans[j].Path
	})
	return overview, nil
}

// addMissing 记录一条缺失文件，并按上限截断清单（总数照实累加）。
func (o *StorageOverview) addMissing(item StorageMissing) {
	o.MissingCount++
	if len(o.Missing) < storageMissingListMax {
		o.Missing = append(o.Missing, item)
	}
}

// CleanupOrphans 删除磁盘上没有任何记录引用的文件（§10）。
//
// 只删孤儿，不碰"库里有记录但文件缺失"的情况（那是对账信息，需要人工判断）。
func (s MediaService) CleanupOrphans(actor PostActor) (StorageCleanupResult, error) {
	overview, err := s.StorageOverview()
	if err != nil {
		return StorageCleanupResult{}, err
	}

	result := StorageCleanupResult{}
	dirs := make(map[string]struct{}, len(overview.Orphans))
	for _, orphan := range overview.Orphans {
		full := ""
		switch orphan.Kind {
		case "attachment":
			full = filepath.Join(global.Config.File.Dir, filepath.FromSlash(orphan.Path))
		default:
			full = filepath.Join(global.Config.Upload.Dir, filepath.FromSlash(orphan.Path))
		}
		if err := os.Remove(full); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				global.Log.Warn("删除孤儿文件失败", "path", orphan.Path, "err", err)
				result.FailedFiles++
				continue
			}
		}
		result.RemovedFiles++
		result.RemovedBytes += orphan.Bytes
		dirs[filepath.Dir(full)] = struct{}{}
	}

	// 顺带收掉被清空的目录；只删空目录，非空目录自然失败。
	for dir := range dirs {
		for {
			if err := os.Remove(dir); err != nil {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir || parent == global.Config.Upload.Dir || parent == global.Config.File.Dir {
				break
			}
			dir = parent
		}
	}

	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionStorageCleanup,
		TargetType: "storage",
		TargetID:   "orphans",
		Meta: map[string]any{
			"removedFiles": result.RemovedFiles,
			"removedBytes": result.RemovedBytes,
			"failedFiles":  result.FailedFiles,
		},
		IP: actor.IP,
	})
	return result, nil
}
