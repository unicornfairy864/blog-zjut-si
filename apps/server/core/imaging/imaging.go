// Package imaging 封装上传图片的解码、EXIF 摆正、缩放与 WebP 编码。
//
// 契约（agent.md §9 / §10）：先 DecodeConfig 再 Decode；按 Orientation 摆正并剥离
// 全部 EXIF（解码后重新编码，元数据自然丢失）；变体不超过原图尺寸，不放大。
package imaging

import (
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
)

// WebPQuality 是变体的编码质量（§10：q82）。
const WebPQuality = 82

// OriginalJPEGQuality 是摆正后原图的 JPEG 质量。
//
// 原图必须重新编码才能剥离 EXIF（§9）；q92 在画质与体积之间取平衡。
const OriginalJPEGQuality = 92

// Config 是探测到的图片元信息，不需要解码像素。
type Config struct {
	Width  int
	Height int
	Pixels int64
	Format string
}

// Probe 只读文件头，返回尺寸与像素数；用于先做上限校验。
func Probe(r io.Reader) (Config, error) {
	config, format, err := image.DecodeConfig(r)
	if err != nil {
		return Config{}, fmt.Errorf("解析图片尺寸失败: %w", err)
	}
	width, height := config.Width, config.Height
	return Config{
		Width:  width,
		Height: height,
		Pixels: int64(width) * int64(height),
		Format: format,
	}, nil
}

// Decode 完整解码图片；支持 JPEG / PNG / GIF / WebP。
func Decode(r io.Reader) (image.Image, string, error) {
	img, format, err := image.Decode(r)
	if err != nil {
		return nil, "", fmt.Errorf("解码图片失败: %w", err)
	}
	return img, format, nil
}

// ResizeToWidth 等比缩放到目标宽度；原图更窄时原样返回（§10：不放大）。
func ResizeToWidth(img image.Image, width int) image.Image {
	bounds := img.Bounds()
	if bounds.Dx() <= width {
		return img
	}
	height := int(math.Round(float64(bounds.Dy()) * float64(width) / float64(bounds.Dx())))
	if height < 1 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, xdraw.Src, nil)
	return dst
}

// EncodeWebP 按固定质量编码 WebP。
func EncodeWebP(w io.Writer, img image.Image) error {
	return webp.Encode(w, img, webp.Options{Quality: WebPQuality})
}

// EncodeOriginal 按上传时的格式重新编码（摆正后调用，EXIF 随编码丢失）。
//
// GIF 只保留首帧；本站不承诺动图。
func EncodeOriginal(w io.Writer, format string, img image.Image) error {
	switch format {
	case "jpeg":
		return jpeg.Encode(w, img, &jpeg.Options{Quality: OriginalJPEGQuality})
	case "png":
		return png.Encode(w, img)
	case "gif":
		return gif.Encode(w, img, nil)
	case "webp":
		return webp.Encode(w, img, webp.Options{Quality: 90})
	default:
		return fmt.Errorf("不支持重新编码的图片格式 %q", format)
	}
}
