import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import type { DockerImage } from './proto/discopanel/v1/minecraft_pb';

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type WithoutChild<T> = T extends { child?: any } ? Omit<T, 'child'> : T;
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type WithoutChildren<T> = T extends { children?: any } ? Omit<T, 'children'> : T;
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

export function getUniqueDockerImages(images: DockerImage[]): DockerImage[] {
	const seen = new Map<string, DockerImage>();
	for (const image of images) {
		if (!seen.has(image.tag)) {
			seen.set(image.tag, image);
		}
	}
	return Array.from(seen.values());
}

export function getDockerImageDisplayName(
	tagOrImage: string | DockerImage,
	dockerImages?: DockerImage[]
): string {
	const image =
		typeof tagOrImage === 'string'
			? dockerImages?.find((img) => img.tag === tagOrImage)
			: tagOrImage;
	if (!image) return tagOrImage as string;
	// Use the displayName field from the generated type
	return image.displayName || image.tag;
}

export function getStringForEnum(map: Record<string, unknown>, val: unknown) {
	return Object.keys(map).find((key) => map[key] === val);
}

// Convert proto enum value to the lowercase string name used by backend
// NOTE: ModLoader.MOD_LOADER_VANILLA (1) -> "vanilla"
export function enumToString(map: Record<string, unknown>, val: unknown): string {
	const enumKey = getStringForEnum(map, val);
	if (!enumKey) return '';
	const parts = enumKey.split('_');
	if (parts.length > 2) {
		return parts.slice(2).join('_').toLowerCase();
	}
	return enumKey.toLowerCase();
}

// Priority weighting for pre-releases: Higher = Newer
const TAG_PRIORITY: Record<string, number> = {
  snapshot: 1,
  pre: 2,
  rc: 3,
  "": 4, // Regular release (no suffix) is the newest
};

interface ParsedVersion {
  base: number[];  // e.g., [26, 2]
  tag: string;     // e.g., "rc", "pre", "snapshot", or ""
  build: number;   // e.g., 2 in "rc-2"
}

function parseVersion(v: string): ParsedVersion {
  // Remove "(Latest)" or similar text from the string
  const cleanV = v.split("(")[0].trim();

  const pv: ParsedVersion = {
    base: [],
    tag: "",
    build: 0,
  };

  // Check if a tag (e.g., "-rc-2") is present
  const [basePart, tagPart] = cleanV.split(/-(.+)/);

  // Parse base version ("26.2" -> [26, 2])
  pv.base = basePart.split(".").map((p) => parseInt(p, 10) || 0);

  // If tag exists ("rc-2" or "snapshot-1")
  if (tagPart) {
    const tagParts = tagPart.split("-");
    pv.tag = tagParts[0]; // e.g., "rc"

    if (tagParts.length > 1) {
      pv.build = parseInt(tagParts[1], 10) || 0; // e.g., 2
    }
  }

  return pv;
}

/**
 * Compares two Minecraft versions.
 * 
 * Returns:
 *  -1 : v1 < v2
 *   0 : v1 == v2
 *   1 : v1 > v2
 */
export function compareMinecraftVersion(v1: string, v2: string): number {
  const pv1 = parseVersion(v1);
  const pv2 = parseVersion(v2);

  // 1. Compare base versions (e.g., 26.3 vs 26.2)
  const maxLen = Math.max(pv1.base.length, pv2.base.length);

  for (let i = 0; i < maxLen; i++) {
    const n1 = pv1.base[i] ?? 0;
    const n2 = pv2.base[i] ?? 0;

    if (n1 < n2) return -1;
    if (n1 > n2) return 1;
  }

  // 2. Compare pre-release tag hierarchy (Release > rc > pre > snapshot)
  const prio1 = TAG_PRIORITY[pv1.tag] ?? 0;
  const prio2 = TAG_PRIORITY[pv2.tag] ?? 0;

  if (prio1 < prio2) return -1;
  if (prio1 > prio2) return 1;

  // 3. If tag is identical (e.g., rc-1 vs rc-2), compare build number
  if (pv1.build < pv2.build) return -1;
  if (pv1.build > pv2.build) return 1;

  return 0;
}
