import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";

// Inter via next/font — downloaded and self-hosted at build time, no runtime
// CDN calls, no tracking. Exposes --font-inter CSS variable consumed by
// tailwind's fontFamily.sans config.
const inter = Inter({
  subsets: ["latin", "latin-ext"],
  display: "swap",
  variable: "--font-inter",
});

export const metadata: Metadata = {
  title: {
    default: "Personel Şeffaflık Portalı",
    template: "%s | Personel Şeffaflık Portalı",
  },
  description:
    "KVKK kapsamında çalışan veri şeffaflığı ve hak kullanım portalı.",
  robots: {
    index: false,  // This portal is internal; no public indexing
    follow: false,
  },
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  return (
    <html suppressHydrationWarning className={inter.variable}>
      <head>
        {/* No external scripts, no analytics, no third-party fonts at runtime */}
        <meta name="referrer" content="strict-origin-when-cross-origin" />
      </head>
      <body className="font-sans">{children}</body>
    </html>
  );
}
