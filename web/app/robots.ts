import type { MetadataRoute } from "next";

export default function robots(): MetadataRoute.Robots {
  return {
    rules: {
      userAgent: "*",
      allow: "/",
      disallow: ["/admin", "/dashboard", "/account", "/rfqs", "/orders", "/chat", "/settings", "/seller"],
    },
    sitemap: "https://switeaz.com/sitemap.xml",
  };
}
