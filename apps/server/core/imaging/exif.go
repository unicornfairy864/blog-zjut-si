package imaging

import (
	"encoding/binary"
	"image"
	"io"
)

// Orientation 读取 JPEG 的 EXIF Orientation（1–8）；非 JPEG 或没有 EXIF 时返回 1。
//
// 只解析 APP1 段里 IFD0 的 0x0112 标签，足够处理手机照片的旋转标记。
func Orientation(r io.Reader) int {
	data, err := io.ReadAll(io.LimitReader(r, 256<<10))
	if err != nil || len(data) < 4 {
		return 1
	}
	if data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}

	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		i += 2

		// 无长度字段的标记：SOI、TEM、RSTn。
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		// SOS 之后是压缩数据，不再有元数据段。
		if marker == 0xDA || i+2 > len(data) {
			return 1
		}

		size := int(binary.BigEndian.Uint16(data[i : i+2]))
		if size < 2 || i+size > len(data) {
			return 1
		}
		segment := data[i+2 : i+size]
		if marker == 0xE1 && len(segment) >= 6 && string(segment[:6]) == "Exif\x00\x00" {
			return orientationFromTIFF(segment[6:])
		}
		i += size
	}
	return 1
}

// orientationFromTIFF 解析 TIFF 结构的 IFD0。
func orientationFromTIFF(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}

	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 1
	}

	offset := int(order.Uint32(tiff[4:8]))
	if offset < 8 || offset+2 > len(tiff) {
		return 1
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	for i := 0; i < count; i++ {
		entry := offset + 2 + i*12
		if entry+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[entry:entry+2]) != 0x0112 {
			continue
		}
		// Orientation 必须是 SHORT 且只有 1 个值。
		if order.Uint16(tiff[entry+2:entry+4]) != 3 || order.Uint32(tiff[entry+4:entry+8]) != 1 {
			return 1
		}
		value := int(order.Uint16(tiff[entry+8 : entry+10]))
		if value >= 1 && value <= 8 {
			return value
		}
		return 1
	}
	return 1
}

// ApplyOrientation 按 EXIF 方向摆正图片；orientation 为 1 时原样返回。
func ApplyOrientation(src image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return src
	}

	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	switch orientation {
	case 2: // 水平翻转
		return remap(src, width, height, func(x, y int) (int, int) { return width - 1 - x, y })
	case 3: // 180°
		return remap(src, width, height, func(x, y int) (int, int) { return width - 1 - x, height - 1 - y })
	case 4: // 垂直翻转
		return remap(src, width, height, func(x, y int) (int, int) { return x, height - 1 - y })
	case 5: // 转置
		return remap(src, height, width, func(x, y int) (int, int) { return y, x })
	case 6: // 顺时针 90°
		return remap(src, height, width, func(x, y int) (int, int) { return y, height - 1 - x })
	case 7: // 反转置
		return remap(src, height, width, func(x, y int) (int, int) { return width - 1 - y, height - 1 - x })
	case 8: // 逆时针 90°
		return remap(src, height, width, func(x, y int) (int, int) { return width - 1 - y, x })
	default:
		return src
	}
}

// remap 以目标坐标反查源坐标生成新图；仅在有 EXIF 旋转时走这里。
func remap(src image.Image, dstWidth, dstHeight int, source func(x, y int) (int, int)) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for y := 0; y < dstHeight; y++ {
		for x := 0; x < dstWidth; x++ {
			sx, sy := source(x, y)
			dst.Set(x, y, src.At(bounds.Min.X+sx, bounds.Min.Y+sy))
		}
	}
	return dst
}
