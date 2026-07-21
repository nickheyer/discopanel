// Reads enum display metadata straight from proto descriptors
import { getOption, hasOption } from '@bufbuild/protobuf';
import type { DescEnum } from '@bufbuild/protobuf';
import type { GenEnum } from '@bufbuild/protobuf/codegenv2';
import { enum_type, value as valueOption } from '$lib/proto/discopanel/options/v1/options_pb';

interface ValueMeta {
	name: string;
	label: string;
	desc: string;
}

const cache = new Map<DescEnum, Map<number, ValueMeta>>();

// Cached metadata table for one enum descriptor
function metaFor(desc: DescEnum): Map<number, ValueMeta> {
	let table = cache.get(desc);
	if (table) return table;
	table = new Map();
	for (const v of desc.values) {
		const meta: ValueMeta = { name: v.localName.toLowerCase(), label: '', desc: '' };
		meta.label = meta.name;
		if (hasOption(v, valueOption)) {
			const ext = getOption(v, valueOption);
			if (ext.name) {
				meta.name = ext.name;
				meta.label = ext.name;
			}
			if (ext.label) meta.label = ext.label;
			meta.desc = ext.desc;
		}
		table.set(v.number, meta);
	}
	cache.set(desc, table);
	return table;
}

// Canonical string for an enum value
export function enumName(desc: DescEnum, value: number): string {
	return metaFor(desc).get(value)?.name ?? '';
}

// Display label for an enum value
export function enumLabel(desc: DescEnum, value: number): string {
	return metaFor(desc).get(value)?.label ?? '';
}

// Longer help text for an enum value
export function enumDesc(desc: DescEnum, value: number): string {
	return metaFor(desc).get(value)?.desc ?? '';
}

// Display name for the enum type itself
export function enumTypeLabel(desc: DescEnum): string {
	return hasOption(desc, enum_type) ? getOption(desc, enum_type).label : '';
}

// Enum value matching a canonical name
export function enumFromName<T extends number>(desc: GenEnum<T>, name: string): T | undefined {
	for (const [num, meta] of metaFor(desc)) {
		if (meta.name === name) return num as T;
	}
	return undefined;
}
