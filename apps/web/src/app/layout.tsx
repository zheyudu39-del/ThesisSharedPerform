import type { Metadata, Viewport } from "next";
import { Fraunces, Inter, IBM_Plex_Mono } from "next/font/google";
import { AuthProvider } from "@/components/AuthProvider";
import { SITE_URL } from "@/lib/seo";
import "./globals.css";

const fraunces = Fraunces({
  subsets: ["latin"],
  variable: "--font-display-loaded",
  display: "swap",
  axes: ["SOFT", "WONK"],
});

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-body-loaded",
  display: "swap",
});

const plexMono = IBM_Plex_Mono({
  subsets: ["latin"],
  weight: ["400", "500", "600"],
  variable: "--font-mono-loaded",
  display: "swap",
});

export const metadata: Metadata = {
  // 让各页相对 canonical / Open Graph 地址解析为绝对地址。
  metadataBase: new URL(SITE_URL),
  title: {
    default: "Thesis Shared · 学术论文社区",
    template: "%s · Thesis Shared",
  },
  description: "以人的判断筛选前沿论文，从共读文献到共同成稿的一体化协作空间。",
  openGraph: {
    type: "website",
    siteName: "Thesis Shared",
    locale: "zh_CN",
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#f6f5f1" },
    { media: "(prefers-color-scheme: dark)", color: "#121419" },
  ],
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN" className={`${fraunces.variable} ${inter.variable} ${plexMono.variable}`}>
      <body>
        {/*
          页面内容区淡入：120ms，只做透明度，不做位移。
          整页上浮会让人误以为在滚动，而且会与信息流的卡片入场叠加。
          SSR 首屏同样会播——从纸色到内容，不是从白闪到内容，可以接受。
        */}
        <AuthProvider>
          <div className="anim-page">{children}</div>
        </AuthProvider>
      </body>
    </html>
  );
}
