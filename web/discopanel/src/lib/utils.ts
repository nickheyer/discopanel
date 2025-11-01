import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import type { DockerImageInfo } from "./api/types";

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

export 	function getUniqueDockerImages(images: DockerImageInfo[]): DockerImageInfo[] {
		const seen = new Map<string, DockerImageInfo>();
		for (const image of images) {
			if (!seen.has(image.tag)) {
				seen.set(image.tag, image);
			}
		}
		return Array.from(seen.values());
	}

export 	function getDockerImageDisplayName(tagOrImage: string | DockerImageInfo, dockerImages?: DockerImageInfo[]): string {
		const image = typeof tagOrImage === 'string'
			? dockerImages?.find(img => img.tag === tagOrImage)
			: tagOrImage;
		if (!image) return tagOrImage as string;
		let displayName = `Java ${image.java} (${image.tag})`;
		if (image.distribution !== 'Ubuntu') {
			displayName = `Java ${image.java} ${image.distribution} (${image.tag})`;
		}
		if (image.jvm !== 'Hotspot') {
			displayName = `Java ${image.java} ${image.jvm} (${image.tag})`;
		}
		return displayName;
	}