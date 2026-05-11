export function isImagesRoute(path: string): boolean {
  return path === "/" || path === "/media" || path.startsWith("/media/images");
}

export function isDocumentsRoute(path: string): boolean {
  return path.startsWith("/media/documents");
}

export function isAudioRoute(path: string): boolean {
  return path.startsWith("/media/audio");
}
