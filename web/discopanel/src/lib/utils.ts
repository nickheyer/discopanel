import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import type { DockerImage } from "./proto/discopanel/v1/minecraft_pb";


export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type WithoutChild<T> = T extends { child?: any } ? Omit<T, "child"> : T;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type WithoutChildren<T> = T extends { children?: any } ? Omit<T, "children"> : T;
export type WithoutChildrenOrChild<T> = WithoutChildren<WithoutChild<T>>;
export type WithElementRef<T, U extends HTMLElement = HTMLElement> = T & { ref?: U | null };

export function formatBytes(bytes: number, decimals = 2): string {
  if (bytes === 0) return '0 Bytes';

  const k = 1024;
  const dm = decimals < 0 ? 0 : decimals;
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'];

  const i = Math.floor(Math.log(bytes) / Math.log(k));

  return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

export 	function getUniqueDockerImages(images: DockerImage[]): DockerImage[] {
		const seen = new Map<string, DockerImage>();
		for (const image of images) {
			if (!seen.has(image.tag)) {
				seen.set(image.tag, image);
			}
		}
		return Array.from(seen.values());
	}

export 	function getDockerImageDisplayName(tagOrImage: string | DockerImage, dockerImages?: DockerImage[]): string {
		const image = typeof tagOrImage === 'string'
			? dockerImages?.find(img => img.tag === tagOrImage)
			: tagOrImage;
		if (!image) return tagOrImage as string;
		// Use the displayName field from the generated type
		return image.displayName || image.tag;
	}

export function getStringForEnum(map: any, val: unknown) {
  return Object.keys(map).find(key => map[key] === val);
}

// Convert proto enum value to the lowercase string name used by backend
// NOTE: ModLoader.MOD_LOADER_VANILLA (1) -> "vanilla"
export function enumToString(map: any, val: unknown): string {
  const enumKey = getStringForEnum(map, val);
  if (!enumKey) return '';
  const parts = enumKey.split('_');
  if (parts.length > 2) {
    return parts.slice(2).join('_').toLowerCase();
  }
  return enumKey.toLowerCase();
}

// Validate Docker image reference format
export function isValidImageReferenceFormat(image: string): boolean {
  if (!image || image.trim() === '') {
    return true; // Empty is valid for auto-select
  }

  // Check for invalid whitespace
  if (/\s/.test(image)) {
    return false;
  }

  // Basic format check - should contain at least a namespace/repo
  // Valid formats: repo:tag, registry.io/repo:tag, registry.io/repo, repo
  const hasValidFormat = /^[a-zA-Z0-9\-._/]+(?::[a-zA-Z0-9\-._]+)?$/.test(image);
  return hasValidFormat;
}

// Debounce helper for async validation
export function debounce<TArgs extends unknown[], TReturn>(
  func: (...args: TArgs) => Promise<TReturn>,
  wait: number
): (...args: TArgs) => Promise<TReturn> {
  let timeout: ReturnType<typeof setTimeout> | null = null;

  return (...args: TArgs) => {
    return new Promise((resolve) => {
      if (timeout) {
        clearTimeout(timeout);
      }

      timeout = setTimeout(() => {
        resolve(func(...args));
      }, wait);
    });
  };
}