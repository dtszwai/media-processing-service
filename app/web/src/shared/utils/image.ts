/**
 * Image processing utilities
 */

// Threshold for when to use thumbnail preview instead of full image
export const THUMBNAIL_THRESHOLD = 10 * 1024 * 1024; // 10MB
export const THUMBNAIL_MAX_SIZE = 200; // Max width/height for thumbnail

/**
 * Generate a thumbnail from an image file using canvas.
 * Much faster and more memory-efficient than loading the full image.
 *
 * @param file - The image file to generate thumbnail from
 * @param maxSize - Maximum width/height for the thumbnail (default: 200)
 * @returns Data URL of the thumbnail image
 */
export function generateThumbnail(file: File, maxSize = THUMBNAIL_MAX_SIZE): Promise<string> {
  return new Promise((resolve, reject) => {
    const objectUrl = URL.createObjectURL(file);
    const img = new Image();

    img.onload = () => {
      // Calculate thumbnail dimensions maintaining aspect ratio
      let thumbWidth = img.width;
      let thumbHeight = img.height;

      if (thumbWidth > thumbHeight) {
        if (thumbWidth > maxSize) {
          thumbHeight = Math.round((thumbHeight * maxSize) / thumbWidth);
          thumbWidth = maxSize;
        }
      } else {
        if (thumbHeight > maxSize) {
          thumbWidth = Math.round((thumbWidth * maxSize) / thumbHeight);
          thumbHeight = maxSize;
        }
      }

      // Draw to canvas and export as small JPEG
      const canvas = document.createElement("canvas");
      canvas.width = thumbWidth;
      canvas.height = thumbHeight;
      const ctx = canvas.getContext("2d");
      ctx?.drawImage(img, 0, 0, thumbWidth, thumbHeight);

      URL.revokeObjectURL(objectUrl);
      resolve(canvas.toDataURL("image/jpeg", 0.7));
    };

    img.onerror = () => {
      URL.revokeObjectURL(objectUrl);
      reject(new Error("Failed to load image"));
    };

    img.src = objectUrl;
  });
}

/**
 * Read file as data URL using FileReader
 * Suitable for small files where full resolution is acceptable
 */
export function readFileAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = (e) => {
      const result = e.target?.result;
      if (typeof result === "string") {
        resolve(result);
      } else {
        reject(new Error("Failed to read file"));
      }
    };
    reader.onerror = () => reject(new Error("Failed to read file"));
    reader.readAsDataURL(file);
  });
}

/**
 * Get preview URL for an image file
 * Uses thumbnail for large files, full image for small files
 */
export async function getPreviewUrl(file: File): Promise<string | null> {
  try {
    if (file.size > THUMBNAIL_THRESHOLD) {
      return await generateThumbnail(file);
    } else {
      return await readFileAsDataUrl(file);
    }
  } catch {
    return null;
  }
}
