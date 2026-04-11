import type { Metadata } from "next";
import "./globals.css";

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
    <html suppressHydrationWarning>
      <head>
        {/* No external scripts, no analytics, no third-party fonts */}
        <meta name="referrer" content="strict-origin-when-cross-origin" />
      </head>
      <body>{children}</body>
    </html>
  );
}
