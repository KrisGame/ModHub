import React, { useEffect, useRef } from "react";

interface SkinHeadProps {
  skinUrl?: string;
  username?: string;
  size?: number;
  className?: string;
}

export const SkinHead: React.FC<SkinHeadProps> = ({
  skinUrl,
  username = "Steve",
  size = 40,
  className = "",
}) => {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    ctx.clearRect(0, 0, size, size);
    ctx.imageSmoothingEnabled = false;

    // Default fallback drawer: draw procedural Steve or Alex face
    const drawDefaultFace = () => {
      // Deterministic choice between Steve and Alex based on username
      let hash = 0;
      for (let i = 0; i < username.length; i++) {
        hash = (hash << 5) - hash + username.charCodeAt(i);
        hash |= 0;
      }
      const isAlex = Math.abs(hash) % 2 === 1;

      const p = size / 8; // 8x8 grid

      // Base skin
      ctx.fillStyle = isAlex ? "#eac39e" : "#b58763";
      ctx.fillRect(0, 0, size, size);

      // Hair
      ctx.fillStyle = isAlex ? "#b85c2b" : "#45281b";
      ctx.fillRect(0, 0, size, p * 2);
      ctx.fillRect(0, p * 2, p, p * 2);
      ctx.fillRect(size - p, p * 2, p, p * 2);

      // Eyes
      ctx.fillStyle = "#ffffff";
      ctx.fillRect(p, p * 4, p * 2, p);
      ctx.fillRect(p * 5, p * 4, p * 2, p);

      ctx.fillStyle = isAlex ? "#3e6958" : "#2d3587"; // green for Alex, blue/purple for Steve
      ctx.fillRect(p * 2, p * 4, p, p);
      ctx.fillRect(p * 5, p * 4, p, p);

      // Nose / mouth
      ctx.fillStyle = isAlex ? "#ca8c64" : "#945b3d";
      ctx.fillRect(p * 3, p * 5, p * 2, p);

      ctx.fillStyle = isAlex ? "#ba5947" : "#5d3527";
      ctx.fillRect(p * 2, p * 6, p * 4, p);
    };

    if (!skinUrl) {
      drawDefaultFace();
      return;
    }

    const img = new Image();
    img.crossOrigin = "anonymous";
    img.onload = () => {
      ctx.clearRect(0, 0, size, size);
      ctx.imageSmoothingEnabled = false;

      // Draw head base front: sx=8, sy=8, sw=8, sh=8
      ctx.drawImage(img, 8, 8, 8, 8, 0, 0, size, size);

      // Draw head hat front: sx=40, sy=8, sw=8, sh=8
      ctx.drawImage(img, 40, 8, 8, 8, 0, 0, size, size);
    };

    img.onerror = () => {
      drawDefaultFace();
    };

    img.src = skinUrl;
  }, [skinUrl, username, size]);

  return (
    <canvas
      ref={canvasRef}
      width={size}
      height={size}
      className={className}
      style={{
        width: size,
        height: size,
        borderRadius: 6,
        imageRendering: "pixelated",
        boxShadow: "0 2px 6px rgba(0,0,0,0.3)",
        background: "rgba(0,0,0,0.15)",
        flexShrink: 0,
      }}
      title={username}
    />
  );
};
