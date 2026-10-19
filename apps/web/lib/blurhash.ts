/**
 * blurhash 解码（agent.md §10 / §11）。
 *
 * 只实现解码、且只服务于占位图：不引入新依赖，算法与 blurhash 官方 JS 实现一致
 * （base83 → DC/AC 系数 → sRGB → 双线性采样）。
 */
const DIGITS =
  "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~";

const BASE83 = DIGITS.length;

/** 解一个 base83 字符。 */
function decode83(value: string): number {
  let result = 0;
  for (const char of value) {
    const digit = DIGITS.indexOf(char);
    if (digit < 0) {
      return Number.NaN;
    }
    result = result * BASE83 + digit;
  }
  return result;
}

/** 把 0–255 的 sRGB 值还原成线性光。 */
function srgbToLinear(value: number): number {
  const v = value / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
}

/** 线性光还原回 sRGB 字节。 */
function linearToSrgb(value: number): number {
  const v = value <= 0.0031308 ? value * 12.92 : 1.055 * value ** (1 / 2.4) - 0.055;
  return Math.max(0, Math.min(255, Math.round(v * 255)));
}

/** 解出 DC 分量（整体平均色）。 */
function decodeDC(value: number): [number, number, number] {
  return [
    srgbToLinear(value >> 16),
    srgbToLinear((value >> 8) & 255),
    srgbToLinear(value & 255),
  ];
}

/** 解出一个 AC 分量（量化后的三个通道系数）。 */
function decodeAC(value: number, maximum: number): [number, number, number] {
  const quantR = Math.floor(value / (19 * 19));
  const quantG = Math.floor(value / 19) % 19;
  const quantB = value % 19;
  return [
    ((quantR - 9) / 9) ** 2 * maximum * Math.sign(quantR - 9),
    ((quantG - 9) / 9) ** 2 * maximum * Math.sign(quantG - 9),
    ((quantB - 9) / 9) ** 2 * maximum * Math.sign(quantB - 9),
  ];
}

/**
 * 把 blurhash 解成 RGBA 像素（宽高建议 32×32，够铺满占位区域）。
 *
 * 非法输入返回 null，调用方退化为纯色占位。
 */
export function decodeBlurhash(
  hash: string,
  width: number,
  height: number,
): Uint8ClampedArray | null {
  if (hash.length < 6 || width <= 0 || height <= 0) {
    return null;
  }

  const sizeFlag = decode83(hash[0]);
  if (Number.isNaN(sizeFlag)) {
    return null;
  }
  const numY = Math.floor(sizeFlag / 9) + 1;
  const numX = (sizeFlag % 9) + 1;
  if (hash.length !== 4 + 2 * numX * numY) {
    return null;
  }

  const maximumValue = (decode83(hash[1]) + 1) / 166;
  if (Number.isNaN(maximumValue)) {
    return null;
  }

  const colors: Array<[number, number, number]> = new Array(numX * numY);
  colors[0] = decodeDC(decode83(hash.slice(2, 6)));
  for (let i = 1; i < numX * numY; i += 1) {
    const value = decode83(hash.slice(4 + i * 2, 6 + i * 2));
    if (Number.isNaN(value)) {
      return null;
    }
    colors[i] = decodeAC(value, maximumValue);
  }

  const pixels = new Uint8ClampedArray(width * height * 4);
  const cosX: number[][] = [];
  const cosY: number[][] = [];
  for (let x = 0; x < numX; x += 1) {
    cosX.push(
      Array.from({ length: width }, (_, px) =>
        Math.cos((Math.PI * (px + 0.5) * x) / width),
      ),
    );
  }
  for (let y = 0; y < numY; y += 1) {
    cosY.push(
      Array.from({ length: height }, (_, py) =>
        Math.cos((Math.PI * (py + 0.5) * y) / height),
      ),
    );
  }

  for (let py = 0; py < height; py += 1) {
    for (let px = 0; px < width; px += 1) {
      let r = 0;
      let g = 0;
      let b = 0;
      for (let y = 0; y < numY; y += 1) {
        for (let x = 0; x < numX; x += 1) {
          const basis = cosX[x][px] * cosY[y][py];
          const color = colors[y * numX + x];
          r += color[0] * basis;
          g += color[1] * basis;
          b += color[2] * basis;
        }
      }
      const offset = (py * width + px) * 4;
      pixels[offset] = linearToSrgb(r);
      pixels[offset + 1] = linearToSrgb(g);
      pixels[offset + 2] = linearToSrgb(b);
      pixels[offset + 3] = 255;
    }
  }

  return pixels;
}
