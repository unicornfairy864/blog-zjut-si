// Package imaging 封装上传图片的解码、EXIF 摆正、缩放与 WebP 编码。
//
// 契约（agent.md §9 / §10）：先 DecodeConfig 再 Decode；按 Orientation 摆正并剥离
// 全部 EXIF（解码后重新编码，元数据自然丢失）；变体不超过原图尺寸，不放大。
package imaging

import (
	"fmt"
	"image"
	"io"
	"math"

	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"

	// 注册 JPEG / PNG / GIF 解码器；WebP 由 gen2brain/webp 的 init 注册。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// WebPQuality 是变体的编码质量（§10：q82）。
const WebPQuality = 82

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
