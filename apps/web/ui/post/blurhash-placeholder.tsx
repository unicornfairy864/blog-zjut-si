"use client";

import { useEffect, useRef } from "react";

import { decodeBlurhash } from "@/lib/blurhash";
import { cn } from "@/lib/utils";

/** 解码后的采样尺寸；32×32 足够铺满占位区域，配合轻微模糊看不出像素块。 */
const SAMPLE_SIZE = 32;

export interface BlurhashPlaceholderProps {
  hash: string;
  className?: string;
}

/**
 * blurhash 占位图（agent.md §10）：解码到 canvas，图片加载完成后由父组件淡出。
 */
export function BlurhashPlaceholder({
  hash,
  className,
}: BlurhashPlaceholderProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !hash) {
      return;
    }
    const pixels = decodeBlurhash(hash, SAMPLE_SIZE, SAMPLE_SIZE);
    const context = canvas.getContext("2d");
    if (!pixels || !context) {
      return;
    }
    canvas.width = SAMPLE_SIZE;
    canvas.height = SAMPLE_SIZE;
    const image = context.createImageData(SAMPLE_SIZE, SAMPLE_SIZE);
    image.data.set(pixels);
    context.putImageData(image, 0, 0);
  }, [hash]);

  return (
    <canvas
      ref={canvasRef}
      aria-hidden="true"
      className={cn("h-full w-full scale-110 blur-lg", className)}
    />
  );
}
