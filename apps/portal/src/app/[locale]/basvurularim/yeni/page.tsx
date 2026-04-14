import { redirect } from "next/navigation";

/**
 * Alias route — redirects to the canonical new-DSR page under `haklar/`.
 * Exists so Roadmap item #105's URL (basvurularim/yeni) resolves to the
 * same form without having two copies of the component.
 */
export default async function BasvurularimYeniRedirectPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<never> {
  const { locale } = await params;
  redirect(`/${locale}/haklar/yeni-basvuru`);
}
