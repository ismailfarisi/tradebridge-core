import { ImageResponse } from "next/og";

export const runtime = "edge";
export const alt = "TradeBridge — B2B RFQ marketplace";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default function OpengraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          background: "linear-gradient(135deg, #0f172a 0%, #1e3a8a 100%)",
          color: "#f8fafc",
          fontFamily: "sans-serif",
        }}
      >
        <div style={{ fontSize: 84, fontWeight: 700, letterSpacing: -2 }}>TradeBridge</div>
        <div style={{ fontSize: 36, marginTop: 24, color: "#cbd5e1" }}>
          B2B RFQ marketplace
        </div>
      </div>
    ),
    { ...size }
  );
}
