// Client-side attachment validation, mirroring Core's server-side limits.

export const MAX_FILE_BYTES = 10 * 1024 * 1024;
export const MAX_PER_MESSAGE = 5;
export const ACCEPT = "image/jpeg,image/png,image/webp,image/gif,application/pdf";

const allowed = new Set(ACCEPT.split(","));

/** Returns an error message, or null when the file is acceptable. */
export function validateFile(name: string, type: string, size: number): string | null {
  if (!allowed.has(type)) return `${name}: file type not supported (images and PDFs only)`;
  if (size > MAX_FILE_BYTES) return `${name}: larger than the 10 MB limit`;
  return null;
}
