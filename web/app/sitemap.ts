import type { MetadataRoute } from "next";

const SITE_URL = "https://switeaz.com";

export default function sitemap(): MetadataRoute.Sitemap {
  const routes = ["/", "/pricing", "/marketplace", "/legal/terms", "/legal/privacy", "/legal/cookies"];
  return routes.map((route) => ({
    url: `${SITE_URL}${route}`,
    lastModified: new Date(),
  }));
}
